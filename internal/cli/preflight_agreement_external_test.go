package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestTextToImageChecksEntryEndpointBeforeEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	document := sdxlOpenAPIFixture(t)
	delete(document["paths"].(map[string]any), "/api/v1/queue/{queue_id}/enqueue_batch")
	var enqueues atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", document, animaModelInventory()) {
			return
		}
		if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
			enqueues.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--model", "main-key", "--prompt", "lighthouse", "--seed", "1", "--url", server.URL, "--json"})
	var envelope struct {
		Error *result.Error `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitUnsupportedCapability || envelope.Error == nil || envelope.Error.Code != "unsupported_capability" || envelope.Error.Message != "InvokeAI does not provide required endpoint POST /api/v1/queue/{queue_id}/enqueue_batch" || enqueues.Load() != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d error=%#v enqueues=%d stderr=%q", code, envelope.Error, enqueues.Load(), stderr.String())
	}
}

func TestUpscaleChecksEntryEndpointBeforeUpload(t *testing.T) {
	document := sdxlOpenAPIFixture(t)
	delete(document["paths"].(map[string]any), "/api/v1/queue/{queue_id}/enqueue_batch")
	var uploads, enqueues atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", document, upscaleInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/upload":
			uploads.Add(1)
			acceptUpload(w, r)
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			acceptEnqueue(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	code, envelope := runUpscale(t, server.URL, "upscale", "--no-wait", "--image-path", sourcePNG(t, 512, 256), "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "1")
	if code != result.ExitUnsupportedCapability || envelope.Error == nil || envelope.Error.Code != "unsupported_capability" || envelope.Error.Message != "InvokeAI does not provide required endpoint POST /api/v1/queue/{queue_id}/enqueue_batch" || uploads.Load() != 0 || enqueues.Load() != 0 {
		t.Fatalf("code=%d error=%#v uploads=%d enqueues=%d", code, envelope.Error, uploads.Load(), enqueues.Load())
	}
}

func TestStandaloneRecallUsesCommonSchemaWhileDoctorReportsFamilyField(t *testing.T) {
	isolateUserConfigDir(t)
	document := sdxlOpenAPIFixture(t)
	properties := document["components"].(map[string]any)["schemas"].(map[string]any)["RecallParameter"].(map[string]any)["properties"].(map[string]any)
	delete(properties, "cfg_scale")
	var patches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", document, upscaleInventory()) {
			return
		}
		if r.URL.Path == "/api/v1/recall/default" {
			patches.Add(1)
			_, _ = w.Write([]byte(`{"status":"success"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"recall", "--seed", "1", "--url", server.URL, "--json"})
	if code != result.ExitSuccess || patches.Load() != 1 || stderr.Len() != 0 {
		t.Fatalf("recall code=%d patches=%d stdout=%s stderr=%s", code, patches.Load(), stdout.String(), stderr.String())
	}
	failures := doctorRowFailures(t, server.URL, "recall")
	if len(failures) != 1 || failures[0] != "incompatible_recall_schema:cfg_scale" {
		t.Fatalf("recall row failures=%q", failures)
	}
}

func TestHuggingFaceLoginPreflightAgreesWithDoctor(t *testing.T) {
	isolateUserConfigDir(t)
	for _, test := range []struct {
		name, failure string
		change        func(map[string]any)
	}{
		{"missing endpoint", "missing_endpoint:POST /api/v2/models/hf_login", func(document map[string]any) {
			delete(document["paths"].(map[string]any)["/api/v2/models/hf_login"].(map[string]any), "post")
		}},
		{"incompatible token body", "incompatible_hf_login_schema:token", func(document map[string]any) {
			document["paths"].(map[string]any)["/api/v2/models/hf_login"].(map[string]any)["post"] = map[string]any{}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := sdxlOpenAPIFixture(t)
			test.change(document)
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", document, upscaleInventory()) {
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/api/v2/models/hf_login" {
					posts.Add(1)
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			code, output, stderr := runAuthCommand(t, "hf-test-sentinel\n", "auth", "huggingface", "login", "--token-stdin", "--url", server.URL, "--json")
			envelope := authEnvelope(t, output)
			if code != result.ExitUnsupportedCapability || envelope.Error == nil || envelope.Error.Code != "unsupported_capability" || posts.Load() != 0 || stderr != "" {
				t.Fatalf("login code=%d envelope=%#v posts=%d stderr=%q", code, envelope, posts.Load(), stderr)
			}
			failures := doctorRowFailures(t, server.URL, "auth.huggingface.login")
			if !slices.Contains(failures, test.failure) {
				t.Fatalf("doctor login failures=%q, want %q", failures, test.failure)
			}
		})
	}
}

func doctorRowFailures(t *testing.T, serverURL, operation string) []string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"doctor", "--url", serverURL, "--json"})
	if code != result.ExitUnsupportedCapability || stderr.Len() != 0 {
		t.Fatalf("doctor code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var envelope struct {
		Error struct {
			Details struct {
				Report struct {
					Capabilities []struct {
						Operation string   `json:"operation"`
						Failures  []string `json:"failures"`
					} `json:"capabilities"`
				} `json:"report"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, row := range envelope.Error.Details.Report.Capabilities {
		if row.Operation == operation {
			return row.Failures
		}
	}
	t.Fatalf("doctor omitted %s row", operation)
	return nil
}
