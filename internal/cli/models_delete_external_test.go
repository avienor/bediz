package cli_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

const deleteModelKey = "3f6c1a2e-model-key"

type modelDeleteServer struct {
	*httptest.Server
	requests atomic.Int32
	deletes  atomic.Int32
}

func newModelDeleteServer(t *testing.T, version string, deleteModel http.HandlerFunc) *modelDeleteServer {
	t.Helper()
	server := &modelDeleteServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.requests.Add(1)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/v2/models/i/"+deleteModelKey:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key": deleteModelKey, "name": "Throwaway VAE", "base": "sdxl", "type": "vae", "format": "checkpoint",
				"path": "/server/models/throwaway.safetensors", "source": "private", "file_size": 1024,
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2/models/i/"):
			http.NotFound(w, r)
		case r.Method == http.MethodDelete && r.URL.EscapedPath() == "/api/v2/models/i/"+deleteModelKey:
			server.deletes.Add(1)
			deleteModel(w, r)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func deletedModel(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func TestModelsDeleteRequiresYesBeforeAnyNetworkRequest(t *testing.T) {
	isolateUserConfigDir(t)
	server := newModelDeleteServer(t, "6.14.1", deletedModel)

	exitCode, envelope, stderr := runBoardsJSON(t, "", "models", "delete", deleteModelKey, "--url", server.URL)

	if exitCode != result.ExitInvalidRequest || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if code, _ := errorDetails(t, envelope); envelope["operation"] != "models.delete" || code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if requests := server.requests.Load(); requests != 0 {
		t.Fatalf("sent %d requests, want none", requests)
	}
}

func TestModelsDeleteReturnsTheSummaryTheModelHadBeforeDeletion(t *testing.T) {
	isolateUserConfigDir(t)
	server := newModelDeleteServer(t, "6.14.1", deletedModel)

	exitCode, envelope, stderr := runBoardsJSON(t, "", "models", "delete", deleteModelKey, "--yes", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	want := map[string]any{"key": deleteModelKey, "name": "Throwaway VAE", "base": "sdxl", "type": "vae", "format": "checkpoint"}
	if envelope["operation"] != "models.delete" || !reflect.DeepEqual(envelope["data"], want) {
		t.Fatalf("envelope = %#v, want data %#v", envelope, want)
	}
	if deletes := server.deletes.Load(); deletes != 1 {
		t.Fatalf("sent %d deletes, want one", deletes)
	}
}

func TestModelsDeleteResolvesOnlyAnExactModelKey(t *testing.T) {
	isolateUserConfigDir(t)
	for _, selector := range []string{"Throwaway VAE", "throwaway", "3F6C1A2E-MODEL-KEY"} {
		t.Run(selector, func(t *testing.T) {
			server := newModelDeleteServer(t, "6.14.1", deletedModel)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "models", "delete", selector, "--yes", "--url", server.URL)

			if exitCode != result.ExitInvokeAIFailure || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if code, _ := errorDetails(t, envelope); envelope["operation"] != "models.delete" || code != "not_found" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if deletes := server.deletes.Load(); deletes != 0 {
				t.Fatalf("sent %d deletes, want none", deletes)
			}
		})
	}
}

func TestModelsDeleteAcceptsRequestDocumentWithExecutionApproval(t *testing.T) {
	isolateUserConfigDir(t)
	server := newModelDeleteServer(t, "6.14.1", deletedModel)
	document := `{"schema_version":1,"model_key":"` + deleteModelKey + `"}`

	exitCode, envelope, stderr := runBoardsJSON(t, document, "models", "delete", "--request", "-", "--yes", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if data, _ := envelope["data"].(map[string]any); data["key"] != deleteModelKey || server.deletes.Load() != 1 {
		t.Fatalf("envelope = %#v, deletes = %d", envelope, server.deletes.Load())
	}
}

func TestModelsDeleteRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	document := `{"schema_version":1,"model_key":"` + deleteModelKey + `"}`
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "missing key", args: []string{"--yes"}},
		{name: "missing yes in request document", stdin: document, args: []string{"--request", "-"}},
		{name: "yes inside request document", stdin: `{"schema_version":1,"model_key":"` + deleteModelKey + `","yes":true}`, args: []string{"--request", "-", "--yes"}},
		{name: "key combined with request document", stdin: document, args: []string{deleteModelKey, "--request", "-", "--yes"}},
		{name: "empty document key", stdin: `{"schema_version":1,"model_key":""}`, args: []string{"--request", "-", "--yes"}},
		{name: "unsupported schema version", stdin: `{"schema_version":2,"model_key":"` + deleteModelKey + `"}`, args: []string{"--request", "-", "--yes"}},
		{name: "name selector in document", stdin: `{"schema_version":1,"model_name":"Throwaway VAE"}`, args: []string{"--request", "-", "--yes"}},
		{name: "bulk selector in document", stdin: `{"schema_version":1,"model_key":"` + deleteModelKey + `","keys":["other"]}`, args: []string{"--request", "-", "--yes"}},
		{name: "two keys", args: []string{deleteModelKey, "other-key", "--yes"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newModelDeleteServer(t, "6.14.1", deletedModel)
			args := append([]string{"models", "delete", "--url", server.URL}, test.args...)

			exitCode, envelope, stderr := runBoardsJSON(t, test.stdin, args...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if code, _ := errorDetails(t, envelope); envelope["operation"] != "models.delete" || code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestModelsDeleteRejectsPathLikeKeysBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	for _, key := range []string{"../install", "x/../" + deleteModelKey, "..%2Fscan_folder", `folder\key`, ".", ".."} {
		t.Run(key, func(t *testing.T) {
			server := newModelDeleteServer(t, "6.14.1", deletedModel)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "models", "delete", key, "--yes", "--url", server.URL)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if code, _ := errorDetails(t, envelope); code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestModelsDeleteClassifiesOneMutationFailureWithoutRetry(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name     string
		serve    http.HandlerFunc
		wantCode string
	}{
		{name: "removed before deletion", serve: func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, wantCode: "not_found"},
		{name: "operation in progress", serve: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "busy", http.StatusConflict)
		}, wantCode: "invokeai_operation_failed"},
		{name: "conclusive rejection", serve: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "failed", http.StatusInternalServerError)
		}, wantCode: "invokeai_operation_failed"},
		{name: "gateway status", serve: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}, wantCode: "outcome_unknown"},
		{name: "lost response", serve: func(w http.ResponseWriter, _ *http.Request) {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack delete connection: %v", err)
				return
			}
			_ = connection.Close()
		}, wantCode: "outcome_unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newModelDeleteServer(t, "6.14.1", test.serve)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "models", "delete", deleteModelKey, "--yes", "--url", server.URL)

			if exitCode != result.ExitInvokeAIFailure || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if code, _ := errorDetails(t, envelope); envelope["operation"] != "models.delete" || code != test.wantCode {
				t.Fatalf("unexpected envelope: %#v, want code %q", envelope, test.wantCode)
			}
			if deletes := server.deletes.Load(); deletes != 1 {
				t.Fatalf("sent %d deletes, want one", deletes)
			}
		})
	}
}

func TestModelsDeleteRefusesAnIncompleteOrMismatchedRecordBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	complete := map[string]any{"key": deleteModelKey, "name": "Throwaway VAE", "base": "sdxl", "type": "vae", "format": "checkpoint"}
	tests := map[string]func(record map[string]any){
		"another key":    func(record map[string]any) { record["key"] = "other-key" },
		"missing name":   func(record map[string]any) { delete(record, "name") },
		"missing base":   func(record map[string]any) { delete(record, "base") },
		"missing type":   func(record map[string]any) { delete(record, "type") },
		"missing format": func(record map[string]any) { delete(record, "format") },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			record := maps.Clone(complete)
			change(record)
			var deletes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/app/version":
					_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
				case r.Method == http.MethodGet:
					_ = json.NewEncoder(w).Encode(record)
				default:
					deletes.Add(1)
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()

			exitCode, envelope, stderr := runBoardsJSON(t, "", "models", "delete", deleteModelKey, "--yes", "--url", server.URL)

			if code, _ := errorDetails(t, envelope); exitCode != result.ExitInvokeAIFailure || stderr != "" || code != "invalid_invokeai_response" || deletes.Load() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, deletes = %d, envelope = %#v", exitCode, stderr, deletes.Load(), envelope)
			}
		})
	}
}

func TestModelsDeleteRequiresSupportedInvokeAIVersionBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	for _, version := range []string{"6.14.0", "6.15.0"} {
		t.Run(version, func(t *testing.T) {
			server := newModelDeleteServer(t, version, deletedModel)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "models", "delete", deleteModelKey, "--yes", "--url", server.URL)

			if exitCode != result.ExitUnsupportedCapability || stderr != "" || server.deletes.Load() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, deletes = %d, envelope = %#v", exitCode, stderr, server.deletes.Load(), envelope)
			}
		})
	}
}

func TestModelsDeleteHumanOutputNamesTheDeletedModel(t *testing.T) {
	isolateUserConfigDir(t)
	server := newModelDeleteServer(t, "6.14.1", deletedModel)
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(""), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "delete", deleteModelKey, "--yes", "--url", server.URL})

	if want := deleteModelKey + "\tThrowaway VAE\tsdxl\tvae\tcheckpoint\n"; exitCode != result.ExitSuccess || stderr.Len() != 0 || stdout.String() != want {
		t.Fatalf("exit code = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}
