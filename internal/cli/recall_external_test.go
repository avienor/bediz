package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestRecallPatchFlagsAcceptedWithoutEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	posts := 0
	enqueues := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", recallOpenAPI(), animaModelInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/recall/default":
			posts++
			if r.Method != http.MethodPost || r.URL.RawQuery != "" {
				t.Errorf("recall request = %s %s", r.Method, r.URL.String())
			}
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			want := map[string]any{"model": "Anima Main", "positive_prompt": "a lighthouse", "negative_prompt": "text", "width": float64(768), "height": float64(1024), "steps": float64(24), "seed": float64(42)}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("recall patch = %#v, want %#v", got, want)
			}
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/queue/default/enqueue_batch":
			enqueues++
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"recall", "--model", "main-key", "--prompt", "a lighthouse", "--negative-prompt", "text", "--width", "768", "--height", "1024", "--steps", "24", "--seed", "42", "--url", server.URL, "--json"})
	if code != result.ExitSuccess || stderr.Len() != 0 || posts != 1 || enqueues != 0 {
		t.Fatalf("code=%d stderr=%q stdout=%q posts=%d enqueues=%d", code, stderr.String(), stdout.String(), posts, enqueues)
	}
	var envelope struct {
		SchemaVersion int            `json:"schema_version"`
		OK            bool           `json:"ok"`
		Operation     string         `json:"operation"`
		Data          map[string]any `json:"data"`
		Warnings      []any          `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "recall" || !reflect.DeepEqual(envelope.Data, map[string]any{"queue_id": "default", "mode": "patch"}) || len(envelope.Warnings) != 0 {
		t.Fatalf("envelope=%#v", envelope)
	}
}

func TestRecallPartialDocumentAndValidationBeforeMutation(t *testing.T) {
	tests := []struct {
		name, document, wantCode, wantField string
		args                                []string
	}{
		{"prompt only", `{"schema_version":1,"positive_prompt":"alone"}`, "", "positive_prompt", nil},
		{"empty negative prompt", `{"schema_version":1,"negative_prompt":""}`, "", "negative_prompt", nil},
		{"seed only", `{"schema_version":1,"seed":0}`, "", "seed", nil},
		{"empty patch", `{"schema_version":1}`, "invalid_request", "", nil},
		{"missing height", `{"schema_version":1,"model":"main-key","width":768}`, "invalid_request", "", nil},
		{"steps without model", `{"schema_version":1,"steps":20}`, "invalid_request", "", nil},
		{"bad dimensions", `{"schema_version":1,"model":"main-key","width":770,"height":1024}`, "invalid_request", "", nil},
		{"unknown field", `{"schema_version":1,"scheduler":"heun"}`, "invalid_request", "", nil},
		{"unsupported version", `{"schema_version":2,"seed":1}`, "invalid_request", "", nil},
		{"mixed inputs", `{"schema_version":1,"seed":1}`, "invalid_request", "", []string{"--seed", "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			posts := 0
			enqueues := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", recallOpenAPI(), animaModelInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/recall/default":
					posts++
					var got map[string]any
					if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
						t.Error(err)
					}
					if _, ok := got[tt.wantField]; tt.wantField != "" && !ok {
						t.Errorf("missing %s in %#v", tt.wantField, got)
					}
					_, _ = w.Write([]byte(`{"status":"success"}`))
				case "/api/v1/queue/default/enqueue_batch":
					enqueues++
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			app := cli.NewWithIO(strings.NewReader(tt.document), &stdout, &stderr)
			args := append([]string{"recall", "--request", "-", "--url", server.URL, "--json"}, tt.args...)
			code := app.Run(t.Context(), args)
			var envelope struct {
				OK        bool   `json:"ok"`
				Operation string `json:"operation"`
				Error     *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if tt.wantCode == "" {
				if code != 0 || !envelope.OK || posts != 1 {
					t.Fatalf("code=%d envelope=%#v posts=%d", code, envelope, posts)
				}
			} else if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != tt.wantCode || posts != 0 {
				t.Fatalf("code=%d envelope=%#v posts=%d", code, envelope, posts)
			}
			if envelope.Operation != "recall" || enqueues != 0 || stderr.Len() != 0 {
				t.Fatalf("operation=%s enqueues=%d stderr=%q", envelope.Operation, enqueues, stderr.String())
			}
		})
	}
}

func TestRecallRejectsUnsafeModelsAndUnsupportedContract(t *testing.T) {
	wrongSchema := recallOpenAPI()
	properties := wrongSchema["components"].(map[string]any)["schemas"].(map[string]any)["RecallParameter"].(map[string]any)["properties"].(map[string]any)
	properties["seed"] = map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}}
	tests := []struct {
		name, version string
		inventory     []map[string]any
		openAPI       map[string]any
		args          []string
		wantCode      string
	}{
		{"ambiguous selector", "6.14.1", append(animaModelInventory(), map[string]any{"key": "other", "hash": "h", "name": "Anima Main", "base": "anima", "type": "main"}), recallOpenAPI(), []string{"--model", "Anima Main"}, "selection_required"},
		{"duplicate display name", "6.14.1", append(animaModelInventory(), map[string]any{"key": "other", "hash": "h", "name": "Anima Main", "base": "sdxl", "type": "main"}), recallOpenAPI(), []string{"--model", "main-key"}, "unsupported_capability"},
		{"unsupported version", "6.15.0", animaModelInventory(), recallOpenAPI(), []string{"--seed", "1"}, "unsupported_capability"},
		{"missing recall schema", "6.14.1", animaModelInventory(), map[string]any{}, []string{"--seed", "1"}, "unsupported_capability"},
		{"wrong recall field type", "6.14.1", animaModelInventory(), wrongSchema, []string{"--seed", "1"}, "unsupported_capability"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, tt.version, tt.openAPI, tt.inventory) {
					return
				}
				if r.URL.Path == "/api/v1/recall/default" {
					posts++
					_, _ = w.Write([]byte(`{"status":"success"}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			args := append([]string{"recall", "--url", server.URL, "--json"}, tt.args...)
			code := cli.New(&stdout, &stderr).Run(t.Context(), args)
			var envelope struct {
				Error *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != tt.wantCode || code != result.ExitStatus(tt.wantCode) || posts != 0 || stderr.Len() != 0 {
				t.Fatalf("code=%d envelope=%#v posts=%d stderr=%q", code, envelope, posts, stderr.String())
			}
		})
	}
}

func TestRecallInconclusiveMutationIsNotRetried(t *testing.T) {
	isolateUserConfigDir(t)
	var posts atomic.Int32
	var stdout, stderr bytes.Buffer
	// The preflight remains reachable, then the Recall response is lost.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", recallOpenAPI(), animaModelInventory()) {
			return
		}
		if r.URL.Path == "/api/v1/recall/default" {
			posts.Add(1)
			h, ok := w.(http.Hijacker)
			if !ok {
				t.Error("no hijacker")
				return
			}
			conn, _, err := h.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"recall", "--seed", "1", "--url", server.URL, "--json"})
	var envelope struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitInvokeAIFailure || envelope.Error == nil || envelope.Error.Code != "outcome_unknown" || posts.Load() != 1 || stderr.Len() != 0 {
		t.Fatalf("code=%d envelope=%#v posts=%d stderr=%q", code, envelope, posts.Load(), stderr.String())
	}
}

func recallOpenAPI() map[string]any {
	properties := map[string]any{}
	for _, name := range []string{"positive_prompt", "negative_prompt", "model", "width", "height", "steps", "seed"} {
		valueType := "string"
		if name == "width" || name == "height" || name == "steps" || name == "seed" {
			valueType = "integer"
		}
		properties[name] = map[string]any{"anyOf": []any{map[string]any{"type": valueType}, map[string]any{"type": "null"}}}
	}
	return map[string]any{
		"paths":      map[string]any{"/api/v1/recall/{queue_id}": map[string]any{"post": map[string]any{"requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/RecallParameter"}}}}}}},
		"components": map[string]any{"schemas": map[string]any{"RecallParameter": map[string]any{"properties": properties}}},
	}
}
