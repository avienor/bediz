package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestGenerateDispatchesMainModelsAcrossInstalledFamilies(t *testing.T) {
	for _, base := range []string{"sdxl-refiner", "sd-1"} {
		t.Run(base, func(t *testing.T) {
			isolateUserConfigDir(t)
			inventory := append(animaModelInventory(), map[string]any{
				"key": "other-main", "hash": "other-hash", "name": "Other Main", "base": base, "type": "main",
			})
			var enqueues int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), inventory) {
					return
				}
				if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
					enqueues++
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := cli.New(&stdout, &stderr).Run(t.Context(), []string{
				"generate", "--no-wait", "--model", "other-main", "--prompt", "test", "--url", server.URL, "--json",
			})
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if code != result.ExitUnsupportedCapability || envelope.Error == nil || envelope.Error.Code != result.CodeUnsupportedCapability || enqueues != 0 || stderr.Len() != 0 {
				t.Fatalf("code=%d envelope=%#v enqueues=%d stderr=%q", code, envelope, enqueues, stderr.String())
			}
		})
	}
}

func TestSD1MainRemainsUnsupportedForGenerateAndRecall(t *testing.T) {
	for _, operationName := range []string{"generate", "recall"} {
		for _, selector := range []string{"sd15-key", "Dreamshaper 8"} {
			t.Run(operationName+"/"+selector, func(t *testing.T) {
				isolateUserConfigDir(t)
				inventory := append(animaModelInventory(), map[string]any{
					"key": "sd15-key", "hash": "sd15-hash", "name": "Dreamshaper 8", "base": "sd-1", "type": "main",
				})
				var mutations int
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), inventory) {
						return
					}
					if r.Method == http.MethodPost {
						mutations++
					}
					http.NotFound(w, r)
				}))
				defer server.Close()
				args := []string{operationName, "--model", selector, "--url", server.URL, "--json"}
				if operationName == "generate" {
					args = append(args, "--prompt", "test", "--no-wait")
				}
				var stdout, stderr bytes.Buffer
				code := cli.New(&stdout, &stderr).Run(t.Context(), args)
				var envelope result.Envelope
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if code != result.ExitUnsupportedCapability || envelope.Error == nil || envelope.Error.Code != result.CodeUnsupportedCapability ||
					envelope.Error.Message != `model family "sd-1" is not supported for generation` || mutations != 0 || stderr.Len() != 0 {
					t.Fatalf("code=%d envelope=%#v mutations=%d stderr=%q", code, envelope, mutations, stderr.String())
				}
			})
		}
	}
}

func TestGenerateAmbiguousMainNameAcrossFamiliesRequiresSelection(t *testing.T) {
	isolateUserConfigDir(t)
	inventory := append(animaModelInventory(), map[string]any{
		"key": "a-other", "hash": "other-hash", "name": "Anima Main", "base": "sdxl", "type": "main",
	})
	var enqueues int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), inventory) {
			return
		}
		if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
			enqueues++
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "Anima Main", "--prompt", "test", "--url", server.URL, "--json",
	})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitSelectionRequired || envelope.Error == nil || envelope.Error.Code != result.CodeSelectionRequired || enqueues != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d envelope=%#v enqueues=%d stderr=%q", code, envelope, enqueues, stderr.String())
	}
	candidates := envelope.Error.Details["candidates"].([]any)
	keys := []string{candidates[0].(map[string]any)["key"].(string), candidates[1].(map[string]any)["key"].(string)}
	if !reflect.DeepEqual(keys, []string{"a-other", "main-key"}) {
		t.Fatalf("candidate keys = %q", keys)
	}
}

func TestGenerateRejectsUnknownComponentBeforeEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueues int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
			return
		}
		if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
			enqueues++
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	request := `{"schema_version":1,"model":"main-key","positive_prompt":"test","components":{"t5_encoder":"t5-key"}}`
	var stdout, stderr bytes.Buffer
	code := cli.NewWithIO(bytes.NewBufferString(request), &stdout, &stderr).Run(t.Context(), []string{
		"generate", "--no-wait", "--request", "-", "--url", server.URL, "--json",
	})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || enqueues != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d envelope=%#v enqueues=%d stderr=%q", code, envelope, enqueues, stderr.String())
	}
}

func TestRecallRejectsUnregisteredMainModelBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	inventory := append(animaModelInventory(), map[string]any{
		"key": "refiner-main", "hash": "refiner-hash", "name": "SDXL Refiner", "base": "sdxl-refiner", "type": "main",
	})
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", recallOpenAPI(), inventory) {
			return
		}
		if r.URL.Path == "/api/v1/recall/default" {
			posts++
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{
		"recall", "--model", "refiner-main", "--url", server.URL, "--json",
	})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitUnsupportedCapability || envelope.Error == nil || envelope.Error.Code != result.CodeUnsupportedCapability || posts != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d envelope=%#v posts=%d stderr=%q", code, envelope, posts, stderr.String())
	}
}

func TestGenerateChecksAnimaSettingsAfterInventoryResolution(t *testing.T) {
	isolateUserConfigDir(t)
	var inventoryReads, enqueues int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/models/" {
			inventoryReads++
		}
		if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
			return
		}
		if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
			enqueues++
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--scheduler", "ddim", "--url", server.URL, "--json",
	})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || inventoryReads != 1 || enqueues != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d envelope=%#v inventory reads=%d enqueues=%d stderr=%q", code, envelope, inventoryReads, enqueues, stderr.String())
	}
}
