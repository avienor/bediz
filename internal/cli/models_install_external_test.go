package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

const huggingFaceInstallOpenAPI = `{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}},"/api/v2/models/hugging_face":{"get":{}}}}`
const pathInstallOpenAPI = `{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"inplace","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`

func installTestServer(t *testing.T, posts *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
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

type huggingFaceTransport func(*http.Request) (*http.Response, error)

func (transport huggingFaceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func mockPublicHuggingFaceRepository(t *testing.T, gated string) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = huggingFaceTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "huggingface.co" {
			if request.URL.String() != "https://huggingface.co/api/models/sample/model" || request.Header.Get("Authorization") != "" {
				t.Errorf("unexpected Hugging Face access check: %s", request.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":` + gated + `,"private":false}`)), Header: make(http.Header)}, nil
		}
		return previous.RoundTrip(request)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func mockCivitaiVersion(t *testing.T, body string) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = huggingFaceTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "civitai.com" {
			if request.URL.String() != "https://civitai.com/api/v1/model-versions/42" {
				t.Errorf("unexpected Civitai metadata request: %s", request.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return previous.RoundTrip(request)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func TestModelsInstallCivitaiAmbiguousFilesUseSelectionEnvelope(t *testing.T) {
	isolateUserConfigDir(t)
	mockCivitaiVersion(t, `{"id":42,"files":[{"id":10,"name":"ten","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=10"},{"id":2,"name":"two","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=2"}]}`)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "civitai", "--source", "42", "--url", server.URL, "--json")
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Kind       string `json:"kind"`
				Selector   int    `json:"selector"`
				Candidates []struct {
					ID      int    `json:"id"`
					Name    string `json:"name"`
					Primary bool   `json:"primary"`
				} `json:"candidates"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	choices := envelope.Error.Details.Candidates
	if code != result.ExitSelectionRequired || stderr != "" || envelope.Error.Code != result.CodeSelectionRequired || envelope.Error.Details.Kind != "civitai_file" || envelope.Error.Details.Selector != 42 || len(choices) != 2 || choices[0].ID != 2 || choices[1].ID != 10 || requests.Load() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q requests=%d", code, stdout, stderr, requests.Load())
	}
}

func TestModelsInstallCivitaiModelPageReturnsVersionChoicesThatResubmitExactly(t *testing.T) {
	isolateUserConfigDir(t)
	const token = "civitai-token-sentinel-742"
	previous := http.DefaultTransport
	http.DefaultTransport = huggingFaceTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "civitai.com" {
			return previous.RoundTrip(request)
		}
		var body string
		switch request.URL.String() {
		case "https://civitai.com/api/v1/models/21":
			body = `{"id":21,"name":"Sample","modelVersions":[{"id":300,"name":"v3.0"},{"id":42,"name":"v1.0"}]}`
		case "https://civitai.com/api/v1/model-versions/300":
			body = `{"id":300,"modelId":21,"files":[{"id":7,"name":"model.safetensors","primary":true,"downloadUrl":"https://civitai.com/api/download/models/300?fileId=7"}]}`
		default:
			t.Errorf("unexpected Civitai metadata request: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("source") != "https://civitai.com/api/download/models/300?fileId=7" {
				t.Errorf("source = %q", r.URL.Query().Get("source"))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":13,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	}))
	t.Cleanup(server.Close)
	page := "https://civitai.com/models/21/sample"
	code, stdout, stderr := runModelCommand(t, token, "models", "install", "--source-type", "civitai", "--source", page, "--token-stdin", "--url", server.URL, "--json")
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		Operation     string `json:"operation"`
		OK            bool   `json:"ok"`
		Error         struct {
			Code    string `json:"code"`
			Details struct {
				Kind       string `json:"kind"`
				Selector   int    `json:"selector"`
				Candidates []struct {
					ID   int    `json:"id"`
					Name string `json:"name"`
				} `json:"candidates"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	choices := envelope.Error.Details.Candidates
	if code != result.ExitSelectionRequired || stderr != "" || envelope.SchemaVersion != 1 || envelope.Operation != "models.install" || envelope.OK ||
		envelope.Error.Code != result.CodeSelectionRequired || envelope.Error.Details.Kind != "civitai_version" || envelope.Error.Details.Selector != 21 ||
		len(choices) != 2 || choices[0].ID != 42 || choices[0].Name != "v1.0" || choices[1].ID != 300 || choices[1].Name != "v3.0" ||
		strings.Contains(stdout, token) || strings.Contains(stdout, page) || strings.Contains(stdout, "civitai.com") || posts.Load() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
	}
	code, stdout, stderr = runModelCommand(t, `{"schema_version":1,"source":{"type":"civitai","reference":"`+strconv.Itoa(choices[1].ID)+`"}}`, "models", "install", "--request", "-", "--url", server.URL, "--json")
	if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"job_id":13`) || posts.Load() != 1 {
		t.Fatalf("resubmit code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
	}
}

func TestModelsInstallCivitaiSelectedFileFlagAndDocumentAgree(t *testing.T) {
	isolateUserConfigDir(t)
	mockCivitaiVersion(t, `{"id":42,"files":[{"id":10,"name":"ten","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=10"},{"id":2,"name":"two","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=2"}]}`)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(pathInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("source") != "https://civitai.com/api/download/models/42?fileId=2" {
				t.Errorf("source = %q", r.URL.Query().Get("source"))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":12,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	}))
	t.Cleanup(server.Close)
	for _, input := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"--source-type", "civitai", "--source", "42", "--file-id", "2"}},
		{`{"schema_version":1,"source":{"type":"civitai","reference":"42","file_id":2}}`, []string{"--request", "-"}},
	} {
		args := append([]string{"models", "install", "--url", server.URL, "--json"}, input.args...)
		code, stdout, stderr := runModelCommand(t, input.stdin, args...)
		if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"job_id":12`) || !strings.Contains(stdout, `"source_type":"civitai"`) {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", input.args, code, stdout, stderr)
		}
	}
	if posts.Load() != 2 {
		t.Fatalf("posts = %d", posts.Load())
	}
}

func TestModelsInstallCivitaiUnknownOutcomeSubmitsOnceWithoutSecrets(t *testing.T) {
	isolateUserConfigDir(t)
	const token = "civitai-token-sentinel-742"
	mockCivitaiVersion(t, `{"id":42,"files":[{"id":7,"name":"model.safetensors","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"}]}`)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("access_token") != token || r.URL.Query().Get("source") != "https://civitai.com/api/download/models/42?fileId=7" {
				t.Error("incorrect Civitai install parameters")
			}
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runModelCommand(t, token, "models", "install", "--source-type", "civitai", "--source", "42", "--token-stdin", "--url", server.URL, "--json")
	if code != result.ExitInvokeAIFailure || stderr != "" || !strings.Contains(stdout, `"code":"outcome_unknown"`) || strings.Contains(stdout, token) || strings.Contains(stdout, "civitai.com") || posts.Load() != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
	}
}

func TestModelsInstallCivitaiMetadataOutageIsSafeConnectionFailure(t *testing.T) {
	isolateUserConfigDir(t)
	const token = "civitai-token-sentinel-742"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	for _, test := range []struct {
		name     string
		response func(*http.Request) (*http.Response, error)
		exit     int
		code     string
	}{
		{"transport failure", func(*http.Request) (*http.Response, error) { return nil, errors.New(token) }, result.ExitConnection, result.CodeConnectionFailed},
		{"service unavailable", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(token)), Header: make(http.Header)}, nil
		}, result.ExitConnection, result.CodeConnectionFailed},
		{"missing version", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(token)), Header: make(http.Header)}, nil
		}, result.ExitInvokeAIFailure, result.CodeNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := http.DefaultTransport
			http.DefaultTransport = huggingFaceTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "civitai.com" {
					return test.response(r)
				}
				return previous.RoundTrip(r)
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			code, stdout, stderr := runModelCommand(t, token, "models", "install", "--source-type", "civitai", "--source", "42", "--token-stdin", "--url", server.URL, "--json")
			if code != test.exit || stderr != "" || !strings.Contains(stdout, `"code":"`+test.code+`"`) || strings.Contains(stdout, token) || strings.Contains(stdout, "civitai.com/api") || requests.Load() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q requests=%d", code, stdout, stderr, requests.Load())
			}
		})
	}
}

func TestHuggingFaceArtifactFlagAndDocumentSubmitSameSelectedArtifact(t *testing.T) {
	isolateUserConfigDir(t)
	mockPublicHuggingFaceRepository(t, `false`)
	const artifact = "https://huggingface.co/sample/model/resolve/main/b.safetensors"
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/a.safetensors","` + artifact + `"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != artifact {
				t.Errorf("unexpected installation: %s %s", r.Method, r.URL)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":4,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	for _, input := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"--source-type", "huggingface", "--source", "sample/model", "--artifact", artifact}},
		{`{"schema_version":1,"source":{"type":"huggingface","reference":"sample/model","artifact":"` + artifact + `"}}`, []string{"--request", "-"}},
	} {
		args := append([]string{"models", "install"}, input.args...)
		args = append(args, "--url", server.URL, "--json")
		code, stdout, stderr := runModelCommand(t, input.stdin, args...)
		if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"job_id":4`) || strings.Contains(stdout, "sample/model") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
	if posts.Load() != 2 {
		t.Fatalf("posts=%d", posts.Load())
	}
}

func TestHuggingFaceInstallFlagsAndDocumentProduceSameCanonicalJob(t *testing.T) {
	isolateUserConfigDir(t)
	mockPublicHuggingFaceRepository(t, `false`)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/model.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != "https://huggingface.co/sample/model" {
				t.Errorf("unexpected installation: %s %s", r.Method, r.URL)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":3,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	for _, input := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"--source-type", "huggingface", "--source", "sample/model"}},
		{`{"schema_version":1,"source":{"type":"huggingface","reference":"https://huggingface.co/sample/model"}}`, []string{"--request", "-"}},
	} {
		args := append([]string{"models", "install"}, input.args...)
		args = append(args, "--url", server.URL, "--json")
		code, stdout, stderr := runModelCommand(t, input.stdin, args...)
		if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"job_id":3`) || !strings.Contains(stdout, `"source_type":"huggingface"`) || strings.Contains(stdout, "sample/model") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
	if posts.Load() != 2 {
		t.Fatalf("posts=%d", posts.Load())
	}
}

func TestGatedHuggingFaceInstallFailsAuthenticationBeforeSubmission(t *testing.T) {
	isolateUserConfigDir(t)
	mockPublicHuggingFaceRepository(t, `"manual"`)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "huggingface", "--source", "sample/model", "--url", server.URL, "--json")
	if code != result.ExitConnection || stderr != "" || !strings.Contains(stdout, `"code":"authentication_failed"`) || posts.Load() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
	}
}

func TestProtectedHuggingFaceInstallAndStatusUseInspectableJobWithoutLeakingToken(t *testing.T) {
	isolateUserConfigDir(t)
	mockPublicHuggingFaceRepository(t, `"manual"`)
	const token = "hf-download-secret-742"
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hf_login":
			_, _ = w.Write([]byte(`"valid"`))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/model.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("access_token") != token || r.URL.Query().Get("source") != "https://huggingface.co/sample/model" {
				t.Error("incorrect protected installation request")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":8,"status":"waiting","source":{"access_token":"` + token + `"}}`))
		case "/api/v2/models/install/8":
			_, _ = w.Write([]byte(`{"id":8,"status":"completed","config_out":{"key":"model-key"},"source":{"access_token":"` + token + `"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runModelCommand(t, token+"\n", "models", "install", "--source-type", "huggingface", "--source", "sample/model", "--token-stdin", "--url", server.URL, "--json")
	if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"job_id":8`) || strings.Contains(stdout, token) || posts.Load() != 1 {
		t.Fatalf("install code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
	}
	code, stdout, stderr = runModelCommand(t, "", "models", "status", "--job-id", "8", "--url", server.URL, "--json")
	if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"model_key":"model-key"`) || strings.Contains(stdout, token) {
		t.Fatalf("status code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
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

func TestModelsInstallProtectedURLUsesTemporaryStdinToken(t *testing.T) {
	isolateUserConfigDir(t)
	const sourceToken = "source-token-sentinel-742"
	const connectionToken = "connection-token-sentinel-851"
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != "https://example.org/protected.safetensors" || r.URL.Query().Get("access_token") != sourceToken || r.Header.Get("Authorization") != "Bearer "+connectionToken || len(r.URL.Query()) != 2 {
				t.Errorf("incorrect protected installation request")
			}
			_, _ = w.Write([]byte(`{"id":9,"status":"waiting","source":{"access_token":"` + sourceToken + `"},"error":"` + sourceToken + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	document := `{"schema_version":1,"source":{"type":"url","reference":"https://example.org/protected.safetensors"}}`
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"models", "install", "--source-type", "url", "--source", "https://example.org/protected.safetensors", "--token-stdin", "--url", server.URL, "--token", connectionToken, "--json"},
		{"models", "install", "--request", path, "--token-stdin", "--url", server.URL, "--token", connectionToken, "--json"},
	} {
		code, stdout, stderr := runModelCommand(t, sourceToken+"\n", args...)
		if code != result.ExitSuccess || !strings.Contains(stdout, `"job_id":9`) || stderr != "" || strings.Contains(stdout+stderr, sourceToken) || strings.Contains(stdout+stderr, connectionToken) {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
	if posts.Load() != 2 {
		t.Fatalf("posts = %d", posts.Load())
	}
}

func TestModelsInstallProtectedURLRejectsMissingTokenAndStdinConflictBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	for _, test := range []struct {
		name  string
		stdin string
		args  []string
	}{
		{"empty", "", []string{"--source-type", "url", "--source", "https://example.org/model", "--token-stdin"}},
		{"whitespace", " \r\n", []string{"--source-type", "url", "--source", "https://example.org/model", "--token-stdin"}},
		{"stdin conflict", "source-token-sentinel-742", []string{"--request", "-", "--token-stdin"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"models", "install"}, test.args...)
			args = append(args, "--url", server.URL, "--json")
			code, stdout, stderr := runModelCommand(t, test.stdin, args...)
			if code != result.ExitInvalidRequest || !strings.Contains(stdout, `"code":"invalid_request"`) || strings.Contains(stdout+stderr, "source-token-sentinel-742") || requests.Load() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q requests=%d", code, stdout, stderr, requests.Load())
			}
		})
	}
}

func TestModelsInstallProtectedURLFailuresNeverRevealTokenOrRetry(t *testing.T) {
	isolateUserConfigDir(t)
	const token = "source-token-sentinel-742"
	for _, test := range []struct {
		name     string
		response func(http.ResponseWriter)
		code     string
		status   int
		human    bool
	}{
		{"authentication", func(w http.ResponseWriter) { http.Error(w, token, http.StatusUnauthorized) }, "authentication_failed", result.ExitConnection, false},
		{"authentication diagnostic", func(w http.ResponseWriter) { http.Error(w, token, http.StatusUnauthorized) }, "authentication_failed", result.ExitConnection, true},
		{"invalid response", func(w http.ResponseWriter) { _, _ = w.Write([]byte(token)) }, "outcome_unknown", result.ExitInvokeAIFailure, false},
		{"lost response", func(w http.ResponseWriter) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}, "outcome_unknown", result.ExitInvokeAIFailure, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
				case "/api/v2/models/install":
					posts.Add(1)
					if r.URL.Query().Get("access_token") != token {
						t.Error("POST did not carry source token")
					}
					test.response(w)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			args := []string{"models", "install", "--source-type", "url", "--source", "https://example.org/model", "--token-stdin", "--url", server.URL}
			if !test.human {
				args = append(args, "--json")
			}
			code, stdout, stderr := runModelCommand(t, token+"\n", args...)
			correctOutput := strings.Contains(stdout, `"code":"`+test.code+`"`) && stderr == ""
			if test.human {
				correctOutput = stdout == "" && strings.Contains(stderr, test.code)
			}
			if code != test.status || !correctOutput || strings.Contains(stdout+stderr, token) || posts.Load() != 1 {
				t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
			}
		})
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

func TestModelsInstallPathFlagsAndDocumentUseSameMoveSetting(t *testing.T) {
	isolateUserConfigDir(t)
	const source = "/server-only/fixtures/model.safetensors"
	var posts atomic.Int32
	var inplaces []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(pathInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("source") != source || r.Method != http.MethodPost {
				t.Errorf("unexpected path submission: %s %s", r.Method, r.URL)
			}
			inplaces = append(inplaces, r.URL.Query().Get("inplace"))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":9,"status":"waiting"}`))
		case "/api/v2/models/install/9":
			_, _ = w.Write([]byte(`{"id":9,"status":"completed","config_out":{"key":"path-model"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	for _, input := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"--source-type", "path", "--source", source}},
		{`{"schema_version":1,"source":{"type":"path","reference":"` + source + `"}}`, []string{"--request", "-"}},
		{"", []string{"--source-type", "path", "--source", source, "--yes"}},
		{`{"schema_version":1,"source":{"type":"path","reference":"` + source + `"},"move":false}`, []string{"--request", "-"}},
		{"", []string{"--source-type", "path", "--source", source, "--move", "--yes"}},
		{`{"schema_version":1,"source":{"type":"path","reference":"` + source + `"},"move":true}`, []string{"--request", "-", "--yes"}},
	} {
		args := append([]string{"models", "install", "--url", server.URL, "--json"}, input.args...)
		code, stdout, stderr := runModelCommand(t, input.stdin, args...)
		if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"source_type":"path"`) || !strings.Contains(stdout, `"job_id":9`) || strings.Contains(stdout, source) {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", input.args, code, stdout, stderr)
		}
	}
	if posts.Load() != 6 || !slices.Equal(inplaces, []string{"true", "true", "true", "true", "false", "false"}) {
		t.Fatalf("posts=%d inplace=%v", posts.Load(), inplaces)
	}
	code, stdout, stderr := runModelCommand(t, "", "models", "status", "--job-id", "9", "--url", server.URL, "--json")
	if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"model_key":"path-model"`) {
		t.Fatalf("status code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestModelsInstallPathMoveWithoutYesDoesNotContactServer(t *testing.T) {
	isolateUserConfigDir(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	for _, input := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"--source-type", "path", "--source", "/server/model.safetensors", "--move"}},
		{`{"schema_version":1,"source":{"type":"path","reference":"/server/model.safetensors"},"move":true}`, []string{"--request", "-"}},
	} {
		args := append([]string{"models", "install", "--url", server.URL, "--json"}, input.args...)
		code, stdout, stderr := runModelCommand(t, input.stdin, args...)
		if code != result.ExitInvalidRequest || stderr != "" || !strings.Contains(stdout, `"code":"invalid_request"`) || !strings.Contains(stdout, "--yes") || requests.Load() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q requests=%d", input.args, code, stdout, stderr, requests.Load())
		}
	}
}

func TestModelsInstallInconclusivePathMoveReportsUnknownOnce(t *testing.T) {
	isolateUserConfigDir(t)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(pathInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("inplace") != "false" {
				t.Error("move was not submitted")
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "path", "--source", "/server/model.safetensors", "--move", "--yes", "--url", server.URL, "--json")
	if code != result.ExitInvokeAIFailure || stderr != "" || !strings.Contains(stdout, `"code":"outcome_unknown"`) || posts.Load() != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts.Load())
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
		{`{"schema_version":1,"source":{"type":"url","reference":"https://civitai.com/api/download/models/42?fileId=7"}}`, nil},
		{`{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model","file_id":1}}`, nil},
		{`{"schema_version":1,"source":{"type":"civitai","reference":"42","file_id":0}}`, nil},
		{`{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model","access_token":"source-token-sentinel-742"}}`, nil},
		{`{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model"},"source_token":"source-token-sentinel-742"}`, nil},
		{`{"schema_version":1,"source":{"type":"path","reference":"org/repo"}}`, nil},
		{`{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model"},"move":false}`, nil},
		{`{"schema_version":1,"source":{"type":"path","reference":"/tmp/model"},"yes":true}`, nil},
		{`{"schema_version":1,"source":{"type":"url","reference":"https://example.org/model"}}`, []string{"--source-type", "url"}},
		{`{"schema_version":1,"source":{"type":"path","reference":"/tmp/model"}}`, []string{"--move"}},
		{`{"schema_version":1,"source":{"type":"civitai","reference":"42"}}`, []string{"--file-id", "2"}},
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
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
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

func TestInstallArgumentErrorsDoNotEchoAccidentalTokens(t *testing.T) {
	isolateUserConfigDir(t)
	const token = "hf_accidental_secret"
	for _, args := range [][]string{
		{"models", "install", token, "--json"},
		{"models", "install", "--token-stdin=" + token, "--json"},
		{"models", "install", token},
		{"models", "install", "--token-stdin=" + token},
	} {
		code, stdout, stderr := runModelCommand(t, "", args...)
		if code != result.ExitInvalidRequest || strings.Contains(stdout+stderr, token) {
			t.Errorf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}
