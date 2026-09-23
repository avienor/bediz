package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func installTestServer(t *testing.T, posts *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}]}}}}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != "https://example.org/model.safetensors" {
				t.Errorf("unexpected install: %s %s", r.Method, r.URL)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":0,"status":"waiting","source":{"access_token":"secret"}}`))
		case "/api/v2/models/install/0":
			_, _ = w.Write([]byte(`{"id":0,"status":"completed","config_out":{"key":"installed-key"},"source":{"access_token":"secret"},"error":"private","error_traceback":"traceback"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func runModelCommand(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(stdin), &stdout, &stderr)
	code := app.Run(t.Context(), args)
	return code, stdout.String(), stderr.String()
}

func TestModelsInstallFlagsAndDocumentProduceSameSafeEnvelope(t *testing.T) {
	isolateUserConfigDir(t)
	var posts atomic.Int32
	server := installTestServer(t, &posts)
	document := `{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model.safetensors"}}`
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"models", "install", "--source-type", "url", "--source", "https://example.org/model.safetensors", "--url", server.URL, "--json"},
		{"models", "install", "--request", path, "--url", server.URL, "--json"},
	} {
		code, stdout, stderr := runModelCommand(t, "", args...)
		if code != result.ExitSuccess || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var envelope struct {
			SchemaVersion int    `json:"schema_version"`
			OK            bool   `json:"ok"`
			Operation     string `json:"operation"`
			Data          struct {
				Jobs []struct {
					JobID      int    `json:"job_id"`
					Status     string `json:"status"`
					SourceType string `json:"source_type"`
					Role       string `json:"role"`
				} `json:"jobs"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "models.install" || len(envelope.Data.Jobs) != 1 || envelope.Data.Jobs[0].JobID != 0 || envelope.Data.Jobs[0].SourceType != "url" || envelope.Data.Jobs[0].Role != "requested" || strings.Contains(stdout, "secret") || strings.Contains(stdout, "example.org") {
			t.Fatalf("envelope: %s", stdout)
		}
	}
	if posts.Load() != 2 {
		t.Fatalf("posts = %d", posts.Load())
	}
}

func TestModelsStatusFlagsAndDocumentProjectCurrentJob(t *testing.T) {
	isolateUserConfigDir(t)
	var posts atomic.Int32
	server := installTestServer(t, &posts)
	for _, input := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"models", "status", "--job-id", "0", "--url", server.URL, "--json"}},
		{`{"schema_version":1,"job_id":0}`, []string{"models", "status", "--request", "-", "--url", server.URL, "--json"}},
	} {
		code, stdout, stderr := runModelCommand(t, input.stdin, input.args...)
		if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"model_key":"installed-key"`) || !strings.Contains(stdout, `"job_id":0`) || strings.Contains(stdout, "secret") || strings.Contains(stdout, "traceback") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
	if posts.Load() != 0 {
		t.Fatalf("posts = %d", posts.Load())
	}
}

func TestModelsInstallRejectsInvalidDocumentsAndMixedFlagsBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	var posts atomic.Int32
	server := installTestServer(t, &posts)
	for _, input := range []struct {
		document string
		flags    []string
	}{
		{`{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model?token=x"}}`, nil},
		{`{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model","file_id":1}}`, nil},
		{`{"schema_version":1,"source":{"type":"path","reference":"/tmp/model"}}`, nil},
		{`{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model"}}`, []string{"--source-type", "url"}},
	} {
		args := append([]string{"models", "install", "--request", "-", "--url", server.URL, "--json"}, input.flags...)
		code, stdout, _ := runModelCommand(t, input.document, args...)
		if code != result.ExitInvalidRequest || !strings.Contains(stdout, `"code":"invalid_request"`) {
			t.Fatalf("code=%d stdout=%q", code, stdout)
		}
	}
	if posts.Load() != 0 {
		t.Fatalf("posts = %d", posts.Load())
	}
}

func TestModelsInstallLostResponseGivesInventoryAndJobInspectionGuidance(t *testing.T) {
	isolateUserConfigDir(t)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}]}}}}`))
		case "/api/v2/models/install":
			posts.Add(1)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		}
	}))
	defer server.Close()
	code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "url", "--source", "https://example.org/model.safetensors", "--url", server.URL, "--json")
	if code != result.ExitInvokeAIFailure || stderr != "" || !strings.Contains(stdout, `"code":"outcome_unknown"`) || !strings.Contains(stdout, "model inventory") || !strings.Contains(stdout, "install job list") || strings.Contains(stdout, "example.org") || posts.Load() != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
	}
}

func TestModelsStatusStructuredFailures(t *testing.T) {
	isolateUserConfigDir(t)
	for _, test := range []struct {
		name          string
		args          []string
		backendStatus int
		backendBody   string
		exit          int
		errorCode     string
	}{
		{"missing ID", nil, 200, `{"id":0,"status":"waiting"}`, result.ExitInvalidRequest, "invalid_request"},
		{"negative ID", []string{"--job-id", "-1"}, 200, `{"id":0,"status":"waiting"}`, result.ExitInvalidRequest, "invalid_request"},
		{"missing job", []string{"--job-id", "0"}, 404, `{"error":"private"}`, result.ExitInvokeAIFailure, "not_found"},
		{"unknown state", []string{"--job-id", "0"}, 200, `{"id":0,"status":"mystery","error":"private"}`, result.ExitInvokeAIFailure, "invalid_invokeai_response"},
		{"reused ID", []string{"--job-id", "0"}, 200, `{"id":8,"status":"completed","config_out":{"key":"wrong"}}`, result.ExitInvokeAIFailure, "invalid_invokeai_response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.backendStatus)
				_, _ = w.Write([]byte(test.backendBody))
			}))
			defer server.Close()
			args := append([]string{"models", "status", "--url", server.URL, "--json"}, test.args...)
			code, stdout, stderr := runModelCommand(t, "", args...)
			if code != test.exit || stderr != "" || !strings.Contains(stdout, `"code":"`+test.errorCode+`"`) || strings.Contains(stdout, "private") || strings.Contains(stdout, "wrong") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestModelsInstallUntestedVersionHasStructuredCapabilityFailure(t *testing.T) {
	isolateUserConfigDir(t)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		_, _ = w.Write([]byte(`{"version":"6.15.0"}`))
	}))
	defer server.Close()
	code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "url", "--source", "https://example.org/model", "--url", server.URL, "--json")
	if code != result.ExitUnsupportedCapability || stderr != "" || !strings.Contains(stdout, `"code":"unsupported_capability"`) || strings.Contains(stdout, "example.org") || posts.Load() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
	}
}

func TestModelsStatusRejectsUnknownDocumentFieldAndMixedFlags(t *testing.T) {
	isolateUserConfigDir(t)
	var posts atomic.Int32
	server := installTestServer(t, &posts)
	for _, input := range []struct {
		document string
		flags    []string
	}{
		{`{"schema_version":1,"job_id":0,"source":"unexpected"}`, nil},
		{`{"schema_version":1,"job_id":0}`, []string{"--job-id", "0"}},
		{`{"schema_version":1,"job_id":-1}`, nil},
	} {
		args := append([]string{"models", "status", "--request", "-", "--url", server.URL, "--json"}, input.flags...)
		code, stdout, _ := runModelCommand(t, input.document, args...)
		if code != result.ExitInvalidRequest || !strings.Contains(stdout, `"code":"invalid_request"`) {
			t.Fatalf("code=%d stdout=%q", code, stdout)
		}
	}
}
