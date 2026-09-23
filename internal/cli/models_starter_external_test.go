package cli_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/avienor/bediz/internal/result"
)

const starterOpenAPI = `{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}},"/api/v2/models/starter_models":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/StarterModelResponse"}}}}}}}}}`

func TestStarterInstallSelectsExactSourceAndReturnsDependencyJobsAndSkips(t *testing.T) {
	isolateUserConfigDir(t)
	var submitted []string
	var submittedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(starterOpenAPI))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(`{"starter_models":[{"name":"Same name","source":"https://example.org/other.safetensors","is_installed":false},{"name":"Same name","source":"https://example.org/main.safetensors","is_installed":false,"dependencies":[{"source":"https://example.org/installed.safetensors","is_installed":true},{"source":"https://example.org/dep.safetensors","is_installed":false}]}],"starter_bundles":{}}`))
		case "/api/v2/models/install":
			submittedMu.Lock()
			submitted = append(submitted, r.URL.Query().Get("source"))
			count := len(submitted)
			submittedMu.Unlock()
			w.WriteHeader(http.StatusCreated)
			if count == 1 {
				_, _ = w.Write([]byte(`{"id":12,"status":"waiting"}`))
			} else {
				_, _ = w.Write([]byte(`{"id":13,"status":"waiting"}`))
			}
		case "/api/v2/models/install/12", "/api/v2/models/install/13":
			if r.URL.Path == "/api/v2/models/install/12" {
				_, _ = w.Write([]byte(`{"id":12,"status":"waiting"}`))
			} else {
				_, _ = w.Write([]byte(`{"id":13,"status":"waiting"}`))
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "starter", "--source", "https://example.org/main.safetensors", "--url", server.URL, "--json")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var envelope struct {
		Data struct {
			Jobs []struct {
				JobID           int    `json:"job_id"`
				SourceType      string `json:"source_type"`
				Role            string `json:"role"`
				DependencyIndex *int   `json:"dependency_index"`
			} `json:"jobs"`
			Skipped []struct {
				Role            string `json:"role"`
				DependencyIndex *int   `json:"dependency_index"`
				Reason          string `json:"reason"`
			} `json:"skipped"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	submittedMu.Lock()
	completedSubmissions := slices.Clone(submitted)
	submittedMu.Unlock()
	if !slices.Equal(completedSubmissions, []string{"https://example.org/dep.safetensors", "https://example.org/main.safetensors"}) || len(envelope.Data.Jobs) != 2 || envelope.Data.Jobs[0].JobID != 12 || envelope.Data.Jobs[0].Role != "dependency" || envelope.Data.Jobs[0].DependencyIndex == nil || *envelope.Data.Jobs[0].DependencyIndex != 1 || envelope.Data.Jobs[1].JobID != 13 || envelope.Data.Jobs[1].Role != "starter" || envelope.Data.Jobs[1].DependencyIndex != nil || envelope.Data.Jobs[1].SourceType != "starter" || len(envelope.Data.Skipped) != 1 || envelope.Data.Skipped[0].Role != "dependency" || envelope.Data.Skipped[0].DependencyIndex == nil || *envelope.Data.Skipped[0].DependencyIndex != 0 || envelope.Data.Skipped[0].Reason != "already_installed" || strings.Contains(stdout, "example.org") {
		t.Fatalf("submitted=%q envelope=%s", completedSubmissions, stdout)
	}
	for _, id := range []string{"12", "13"} {
		statusCode, statusOutput, statusError := runModelCommand(t, "", "models", "status", "--job-id", id, "--url", server.URL, "--json")
		if statusCode != result.ExitSuccess || statusError != "" || !strings.Contains(statusOutput, `"job_id":`+id) {
			t.Fatalf("status %s: code=%d stdout=%q stderr=%q", id, statusCode, statusOutput, statusError)
		}
	}
}

func TestStarterInstallAllInstalledSkipsUnsupportedSources(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(starterOpenAPI))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(`{"starter_models":[{"source":"org/model::variant","is_installed":true,"dependencies":[{"source":"org/dep::subfolder","is_installed":true}]}],"starter_bundles":{}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	for _, input := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"--source-type", "starter", "--source", "org/model::variant"}},
		{`{"schema_version":1,"source":{"type":"starter","reference":"org/model::variant"}}`, []string{"--request", "-"}},
	} {
		args := append([]string{"models", "install"}, input.args...)
		args = append(args, "--url", server.URL, "--json")
		code, stdout, stderr := runModelCommand(t, input.stdin, args...)
		if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, `"jobs":[]`) || !strings.Contains(stdout, `"skipped":[{"role":"dependency","dependency_index":0,"reason":"already_installed"},{"role":"starter","reason":"already_installed"}]`) || strings.Contains(stdout, "org/") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
	code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "starter", "--source", "org/model::variant", "--url", server.URL)
	if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, "Already installed: dependency 0") || !strings.Contains(stdout, "Already installed: starter") || strings.Contains(stdout, "org/") {
		t.Fatalf("human code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestStarterInstallPartialSubmissionsPreserveAcceptedJobsAndSkips(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		code        string
		failedKey   string
		failedIndex string
	}{
		{name: "rejected", status: http.StatusUnprocessableEntity, code: "invokeai_operation_failed", failedKey: "rejected_role", failedIndex: "rejected_dependency_index"},
		{name: "concurrent conflict", status: http.StatusConflict, code: "invokeai_operation_failed", failedKey: "rejected_role", failedIndex: "rejected_dependency_index"},
		{name: "inconclusive", code: "outcome_unknown", failedKey: "uncertain_role", failedIndex: "uncertain_dependency_index"},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var submitted []string
			var submittedMu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					_, _ = w.Write([]byte(starterOpenAPI))
				case "/api/v2/models/starter_models":
					_, _ = w.Write([]byte(`{"starter_models":[{"source":"https://example.org/main","is_installed":false,"dependencies":[{"source":"https://example.org/dep0","is_installed":false},{"source":"https://example.org/installed","is_installed":true},{"source":"https://example.org/dep2","is_installed":false}]}],"starter_bundles":{}}`))
				case "/api/v2/models/install":
					submittedMu.Lock()
					submitted = append(submitted, r.URL.Query().Get("source"))
					count := len(submitted)
					submittedMu.Unlock()
					if count == 1 {
						w.WriteHeader(http.StatusCreated)
						_, _ = w.Write([]byte(`{"id":41,"status":"waiting"}`))
					} else if test.status != 0 {
						w.WriteHeader(test.status)
						_, _ = w.Write([]byte(`{"detail":"https://example.org/private?token=secret"}`))
					} else {
						connection, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = connection.Close()
					}
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
			}))
			t.Cleanup(server.Close)
			code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "starter", "--source", "https://example.org/main", "--url", server.URL, "--json")
			var envelope struct {
				Error struct {
					Code    string                    `json:"code"`
					Details map[string]jsontext.Value `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
				t.Fatal(err)
			}
			submittedMu.Lock()
			completedSubmissions := slices.Clone(submitted)
			submittedMu.Unlock()
			if code != result.ExitInvokeAIFailure || stderr != "" || envelope.Error.Code != test.code || !slices.Equal(completedSubmissions, []string{"https://example.org/dep0", "https://example.org/dep2"}) || string(envelope.Error.Details[test.failedKey]) != `"dependency"` || string(envelope.Error.Details[test.failedIndex]) != `2` || !strings.Contains(string(envelope.Error.Details["jobs"]), `"job_id":41`) || !strings.Contains(string(envelope.Error.Details["skipped"]), `"dependency_index":1`) || strings.Contains(stdout, "example.org") || strings.Contains(stdout, "secret") {
				t.Fatalf("code=%d stdout=%q stderr=%q submitted=%q", code, stdout, stderr, completedSubmissions)
			}
			if test.status == http.StatusConflict && string(envelope.Error.Details["status"]) != `409` {
				t.Fatalf("missing conflict status: %s", stdout)
			}
		})
	}
}

func TestStarterInstallPreflightsEveryMissingDependencyBeforeMutation(t *testing.T) {
	for _, unsafeSource := range []string{
		"sample/dep::fp16", "sample/dep/subfolder", "https://huggingface.co/sample/dep/tree/main", "https://huggingface.co/sample/dep/tree/main/resolve/file.safetensors", "https://huggingface.co/sample/dep/resolve/", "https://example.org/file?token=secret",
	} {
		t.Run(unsafeSource, func(t *testing.T) {
			isolateUserConfigDir(t)
			var posts int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					_, _ = w.Write([]byte(starterOpenAPI))
				case "/api/v2/models/starter_models":
					catalog := `{"starter_models":[{"source":"https://example.org/main","is_installed":false,"dependencies":[{"source":"https://example.org/safe","is_installed":false},{"source":` + mustJSON(t, unsafeSource) + `,"is_installed":false}]}],"starter_bundles":{}}`
					_, _ = w.Write([]byte(catalog))
				case "/api/v2/models/install":
					posts++
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
			}))
			t.Cleanup(server.Close)
			code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "starter", "--source", "https://example.org/main", "--url", server.URL, "--json")
			if code != result.ExitUnsupportedCapability || !strings.Contains(stdout, `"code":"unsupported_capability"`) || stderr != "" || posts != 0 || strings.Contains(stdout, "example.org") || strings.Contains(stdout, "secret") {
				t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts)
			}
		})
	}
}

func TestStarterInstallRejectsTokenAcrossDifferentOriginsBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	const token = "starter-secret-sentinel"
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(strings.Replace(starterOpenAPI, `{"name":"source","in":"query","required":true}`, `{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}`, 1)))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(`{"starter_models":[{"source":"https://example.org/main","is_installed":false,"dependencies":[{"source":"https://other.example/dep","is_installed":false}]}],"starter_bundles":{}}`))
		case "/api/v2/models/install":
			posts++
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runModelCommand(t, token+"\n", "models", "install", "--source-type", "starter", "--source", "https://example.org/main", "--token-stdin", "--url", server.URL, "--json")
	if code != result.ExitUnsupportedCapability || !strings.Contains(stdout, `"code":"unsupported_capability"`) || stderr != "" || posts != 0 || strings.Contains(stdout+stderr, token) || strings.Contains(stdout, "other.example") {
		t.Fatalf("code=%d stdout=%q stderr=%q posts=%d", code, stdout, stderr, posts)
	}
}

func TestStarterInstallAcceptsExactHuggingFaceArtifactInSubdirectory(t *testing.T) {
	isolateUserConfigDir(t)
	mockPublicHuggingFaceRepository(t, "false")
	const source = "https://huggingface.co/sample/model/resolve/main/split_files/model.safetensors"
	var submitted string
	var submittedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(starterOpenAPI))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(`{"starter_models":[{"source":"` + source + `","is_installed":false}],"starter_bundles":{}}`))
		case "/api/v2/models/install":
			submittedMu.Lock()
			submitted = r.URL.Query().Get("source")
			submittedMu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":32,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runModelCommand(t, "", "models", "install", "--source-type", "starter", "--source", source, "--url", server.URL, "--json")
	submittedMu.Lock()
	actualSource := submitted
	submittedMu.Unlock()
	if code != result.ExitSuccess || stderr != "" || actualSource != source || !strings.Contains(stdout, `"job_id":32`) || strings.Contains(stdout, source) {
		t.Fatalf("code=%d stdout=%q stderr=%q submitted=%q", code, stdout, stderr, actualSource)
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
