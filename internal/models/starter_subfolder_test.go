package models_test

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
)

const subfolderStarterOpenAPI = `{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}},"/api/v2/models/starter_models":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/StarterModelResponse"}}}}}}}}}`

func TestStarterSubfolderInstallsFolderAndFileInCatalogOrder(t *testing.T) {
	const starter = "InvokeAI/flux_schnell::transformer/bnb_nf4/flux1-schnell-bnb_nf4.safetensors"
	const dependency = "InvokeAI/t5-v1_1-xxl::bnb_llm_int8"
	var submitted []string
	var submittedMu sync.Mutex
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(subfolderStarterOpenAPI))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(`{"starter_models":[{"source":"` + starter + `","is_installed":false,"dependencies":[{"source":"` + dependency + `","is_installed":false}]}]}`))
		case "/api/v2/models/install":
			submittedMu.Lock()
			submitted = append(submitted, r.URL.Query().Get("source"))
			submittedMu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":12,"status":"waiting"}`))
		default:
			t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL)
		}
	})
	checked := make(map[string]bool)
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("anonymous Hugging Face check carried authorization")
		}
		checked[r.URL.String()] = true
		body := ""
		switch r.URL.String() {
		case "https://huggingface.co/api/models/InvokeAI/t5-v1_1-xxl", "https://huggingface.co/api/models/InvokeAI/flux_schnell":
			body = `{"gated":false,"private":false}`
		case "https://huggingface.co/api/models/InvokeAI/t5-v1_1-xxl/tree/main":
			body = `[{"path":"bnb_llm_int8","type":"directory"}]`
		case "https://huggingface.co/api/models/InvokeAI/t5-v1_1-xxl/tree/main/bnb_llm_int8?recursive=true":
			body = `[{"path":"bnb_llm_int8/weights/model.safetensors","type":"file"}]`
		case "https://huggingface.co/api/models/InvokeAI/flux_schnell/tree/main/transformer/bnb_nf4":
			body = `[{"path":"transformer/bnb_nf4/flux1-schnell-bnb_nf4.safetensors","type":"file"}]`
		default:
			t.Errorf("unexpected Hugging Face request: %s", r.URL)
			return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	got, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: starter}})
	if err != nil {
		t.Fatal(err)
	}
	submittedMu.Lock()
	actualSubmissions := slices.Clone(submitted)
	submittedMu.Unlock()
	if !slices.Equal(actualSubmissions, []string{dependency, starter}) || len(got.Jobs) != 2 || got.Jobs[0].Role != "dependency" || got.Jobs[0].DependencyIndex == nil || *got.Jobs[0].DependencyIndex != 0 || got.Jobs[1].Role != "starter" {
		t.Fatalf("submitted=%q jobs=%#v checked=%v", actualSubmissions, got.Jobs, checked)
	}
	for _, required := range []string{
		"https://huggingface.co/api/models/InvokeAI/t5-v1_1-xxl",
		"https://huggingface.co/api/models/InvokeAI/t5-v1_1-xxl/tree/main",
		"https://huggingface.co/api/models/InvokeAI/t5-v1_1-xxl/tree/main/bnb_llm_int8?recursive=true",
		"https://huggingface.co/api/models/InvokeAI/flux_schnell",
		"https://huggingface.co/api/models/InvokeAI/flux_schnell/tree/main/transformer/bnb_nf4",
	} {
		if !checked[required] {
			t.Errorf("missing preflight request %s", required)
		}
	}
}

func TestStarterSubfolderFolderStarterAndFileDependencyUseTreeForDiffusers(t *testing.T) {
	const starter = "sample/model::transformer/bnb_nf4"
	const dependency = "sample/model::ae.safetensors"
	catalog := `{"starter_models":[{"source":"` + starter + `","is_installed":false,"dependencies":[{"source":"` + dependency + `","is_installed":false}]}]}`
	var metadataCalls atomic.Int32
	var submitted []string
	var submittedMu sync.Mutex
	installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("anonymous tree request had authorization")
		}
		switch r.URL.String() {
		case "https://huggingface.co/api/models/sample/model/tree/main":
			return treeResponse(http.StatusOK, `[{"path":"ae.safetensors","type":"file"},{"path":"transformer","type":"directory"}]`, "")
		case "https://huggingface.co/api/models/sample/model/tree/main/transformer":
			return treeResponse(http.StatusOK, `[{"path":"transformer/bnb_nf4","type":"directory"}]`, "")
		case "https://huggingface.co/api/models/sample/model/tree/main/transformer/bnb_nf4?recursive=true":
			return treeResponse(http.StatusOK, `[{"path":"transformer/bnb_nf4/nested/model.safetensors","type":"file"}]`, "")
		case "https://huggingface.co/api/models/sample/model":
			return treeResponse(http.StatusOK, `{"gated":false,"private":false}`, "")
		default:
			t.Errorf("unexpected Hugging Face request: %s", r.URL)
			return treeResponse(http.StatusNotFound, "", "")
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/models/hugging_face" {
			metadataCalls.Add(1)
			_, _ = w.Write([]byte(`{"urls":null,"is_diffusers":true}`))
			return
		}
		if r.URL.Path != "/api/v2/models/install" {
			t.Errorf("unexpected backend request: %s %s", r.Method, r.URL)
			return
		}
		submittedMu.Lock()
		submitted = append(submitted, r.URL.Query().Get("source"))
		submittedMu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"status":"waiting"}`))
	})
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: starter}})
	submittedMu.Lock()
	actual := slices.Clone(submitted)
	submittedMu.Unlock()
	if err != nil || posts.Load() != 2 || metadataCalls.Load() != 0 || !slices.Equal(actual, []string{dependency, starter}) {
		t.Fatalf("error=%v posts=%d metadata_calls=%d submitted=%q", err, posts.Load(), metadataCalls.Load(), actual)
	}
}

func subfolderTestInstaller(t *testing.T, catalog string, hf func(*http.Request) (*http.Response, error), backend func(http.ResponseWriter, *http.Request)) (models.Installer, *atomic.Int32) {
	t.Helper()
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(subfolderStarterOpenAPI))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(catalog))
		case "/api/v2/models/install":
			posts.Add(1)
			if backend != nil {
				backend(w, r)
			} else {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":1,"status":"waiting"}`))
			}
		default:
			if backend != nil {
				backend(w, r)
			} else {
				t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL)
			}
		}
	})
	return models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(hf)}}, &posts
}

func treeResponse(status int, body string, link string) (*http.Response, error) {
	header := make(http.Header)
	if link != "" {
		header.Set("Link", link)
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: header}, nil
}

func TestStarterSubfolderRejectsUnsafeShapesBeforeMutation(t *testing.T) {
	const starter = "https://example.org/main.safetensors"
	for _, source := range []string{
		"sample/model:fp16::file", "sample/model::folder+other", "sample/model::/absolute", "sample/model::a/../file", "sample/model::a//file", "sample/model::a/./file", `sample/model::a\file`, "sample/model::a:other", "sample/model::", "sample//model::file", "sample/model::a::file",
	} {
		t.Run(source, func(t *testing.T) {
			catalog := `{"starter_models":[{"source":"` + starter + `","is_installed":false,"dependencies":[{"source":"` + strings.ReplaceAll(source, `\`, `\\`) + `","is_installed":false}]}]}`
			installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
				t.Errorf("Hugging Face request before shape validation: %s", r.URL)
				return treeResponse(http.StatusNotFound, "", "")
			}, nil)
			_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: starter}})
			if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
				t.Fatalf("source=%q error=%v posts=%d", source, err, posts.Load())
			}
		})
	}
}

func TestStarterSubfolderRequiresVerifiedTreeEntryBeforeMutation(t *testing.T) {
	const source = "sample/model::folder/file.safetensors"
	for _, test := range []struct {
		name   string
		status int
		body   string
		error  bool
	}{
		{"missing", http.StatusOK, `[{"path":"folder/other.safetensors","type":"file"}]`, false},
		{"unknown type", http.StatusOK, `[{"path":"folder/file.safetensors","type":"symlink"}]`, false},
		{"404", http.StatusNotFound, `{"error":"private"}`, false},
		{"denied", http.StatusForbidden, `{"error":"private"}`, false},
		{"malformed", http.StatusOK, `{"error":"private"}`, false},
		{"incomplete entry", http.StatusOK, `[{}]`, false},
		{"transport failure", 0, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := `{"starter_models":[{"source":"` + source + `","is_installed":false}]}`
			installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/models/sample/model/tree/main/folder" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected tree request: %s", r.URL)
				}
				if test.error {
					return nil, errors.New("private failure")
				}
				return treeResponse(test.status, test.body, "")
			}, nil)
			_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: source}})
			if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 || strings.Contains(err.Error(), "private") {
				t.Fatalf("error=%v posts=%d", err, posts.Load())
			}
		})
	}
}

func TestStarterSubfolderFolderMustContainFileInRecursiveTree(t *testing.T) {
	const source = "sample/model::folder"
	for _, recursiveBody := range []string{`[]`, `[{"path":"folder/nested","type":"directory"}]`, `[{"path":"folder/file.safetensors","type":"file"},{}]`} {
		t.Run(recursiveBody, func(t *testing.T) {
			catalog := `{"starter_models":[{"source":"` + source + `","is_installed":false}]}`
			installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
				switch r.URL.String() {
				case "https://huggingface.co/api/models/sample/model/tree/main":
					return treeResponse(http.StatusOK, `[{"path":"folder","type":"directory"}]`, "")
				case "https://huggingface.co/api/models/sample/model/tree/main/folder?recursive=true":
					return treeResponse(http.StatusOK, recursiveBody, "")
				default:
					t.Errorf("unexpected Hugging Face request: %s", r.URL)
					return treeResponse(http.StatusNotFound, "", "")
				}
			}, nil)
			_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: source}})
			if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
				t.Fatalf("error=%v posts=%d", err, posts.Load())
			}
		})
	}
}

func TestStarterSubfolderFollowsParentTreePagination(t *testing.T) {
	const source = "sample/model::file.safetensors"
	catalog := `{"starter_models":[{"source":"` + source + `","is_installed":false}]}`
	var pages atomic.Int32
	installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://huggingface.co/api/models/sample/model/tree/main":
			pages.Add(1)
			return treeResponse(http.StatusOK, `[{"path":"first.txt","type":"file"}]`, `<https://huggingface.co/api/models/sample/model/tree/main?cursor=page2>; rel="next"`)
		case "https://huggingface.co/api/models/sample/model/tree/main?cursor=page2":
			pages.Add(1)
			return treeResponse(http.StatusOK, `[{"path":"file.safetensors","type":"file"}]`, "")
		case "https://huggingface.co/api/models/sample/model":
			return treeResponse(http.StatusOK, `{"gated":false,"private":false}`, "")
		default:
			t.Errorf("unexpected Hugging Face request: %s", r.URL)
			return treeResponse(http.StatusNotFound, "", "")
		}
	}, nil)
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: source}})
	if err != nil || posts.Load() != 1 || pages.Load() != 2 {
		t.Fatalf("error=%v posts=%d pages=%d", err, posts.Load(), pages.Load())
	}
}

func TestStarterSubfolderProtectedRepositoryRequiresValidStoredLogin(t *testing.T) {
	const source = "sample/private::weights/model.safetensors"
	catalog := `{"starter_models":[{"source":"` + source + `","is_installed":false}]}`
	for _, status := range []string{"valid", "invalid", "unknown"} {
		t.Run(status, func(t *testing.T) {
			var authChecks atomic.Int32
			installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" {
					t.Error("tree API received a token")
				}
				switch r.URL.String() {
				case "https://huggingface.co/api/models/sample/private/tree/main/weights":
					return treeResponse(http.StatusOK, `[{"path":"weights/model.safetensors","type":"file"}]`, "")
				case "https://huggingface.co/api/models/sample/private":
					return treeResponse(http.StatusOK, `{"gated":"auto","private":false}`, "")
				default:
					t.Errorf("unexpected Hugging Face request: %s", r.URL)
					return treeResponse(http.StatusNotFound, "", "")
				}
			}, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/models/install" {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id":1,"status":"waiting"}`))
					return
				}
				if r.URL.Path != "/api/v2/models/hf_login" || r.Method != http.MethodGet {
					t.Errorf("unexpected backend request: %s %s", r.Method, r.URL)
				}
				authChecks.Add(1)
				_, _ = w.Write([]byte(`"` + status + `"`))
			})
			_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: source}})
			if status == "valid" {
				if err != nil || posts.Load() != 1 || authChecks.Load() != 1 {
					t.Fatalf("error=%v posts=%d auth=%d", err, posts.Load(), authChecks.Load())
				}
			} else if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 || authChecks.Load() != 1 {
				t.Fatalf("error=%v posts=%d auth=%d", err, posts.Load(), authChecks.Load())
			}
		})
	}
}

func TestStarterSubfolderRejectsSourceTokenBeforeMutation(t *testing.T) {
	const source = "sample/model::file.safetensors"
	catalog := `{"starter_models":[{"source":"` + source + `","is_installed":false}]}`
	installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
		t.Errorf("Hugging Face request with source token: %s", r.URL)
		return treeResponse(http.StatusNotFound, "", "")
	}, nil)
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: source}, SourceToken: "private-token"})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestStarterSubfolderLaterDependencyTreeFailurePreventsAllJobs(t *testing.T) {
	const starter = "https://example.org/main.safetensors"
	catalog := `{"starter_models":[{"source":"` + starter + `","is_installed":false,"dependencies":[{"source":"sample/model::good.safetensors","is_installed":false},{"source":"sample/model::missing.safetensors","is_installed":false}]}]}`
	installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://huggingface.co/api/models/sample/model/tree/main":
			return treeResponse(http.StatusOK, `[{"path":"good.safetensors","type":"file"}]`, "")
		case "https://huggingface.co/api/models/sample/model":
			return treeResponse(http.StatusOK, `{"gated":false,"private":false}`, "")
		default:
			t.Errorf("unexpected Hugging Face request: %s", r.URL)
			return treeResponse(http.StatusNotFound, "", "")
		}
	}, nil)
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: starter}})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestStarterSubfolderAlreadyInstalledSkipsValidation(t *testing.T) {
	const source = "sample/model::../unsafe"
	catalog := `{"starter_models":[{"source":"` + source + `","is_installed":true}]}`
	installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
		t.Errorf("Hugging Face request for installed source: %s", r.URL)
		return treeResponse(http.StatusNotFound, "", "")
	}, nil)
	got, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: source}})
	if err != nil || posts.Load() != 0 || len(got.Jobs) != 0 || got.Skipped == nil || len(*got.Skipped) != 1 || (*got.Skipped)[0].Reason != "already_installed" {
		t.Fatalf("result=%#v error=%v posts=%d", got, err, posts.Load())
	}
}

func TestStarterSubfolderPartialSubmissionKeepsAcceptedJobWithoutReplay(t *testing.T) {
	const starter = "sample/model::main.safetensors"
	catalog := `{"starter_models":[{"source":"` + starter + `","is_installed":false,"dependencies":[{"source":"sample/model::dep.safetensors","is_installed":false}]}]}`
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "uncertain"}[uncertain], func(t *testing.T) {
			var submissionCount atomic.Int32
			installer, posts := subfolderTestInstaller(t, catalog, func(r *http.Request) (*http.Response, error) {
				switch r.URL.String() {
				case "https://huggingface.co/api/models/sample/model/tree/main":
					return treeResponse(http.StatusOK, `[{"path":"dep.safetensors","type":"file"},{"path":"main.safetensors","type":"file"}]`, "")
				case "https://huggingface.co/api/models/sample/model":
					return treeResponse(http.StatusOK, `{"gated":false,"private":false}`, "")
				default:
					t.Errorf("unexpected Hugging Face request: %s", r.URL)
					return treeResponse(http.StatusNotFound, "", "")
				}
			}, func(w http.ResponseWriter, r *http.Request) {
				if submissionCount.Add(1) == 1 {
					if r.URL.Query().Get("source") != "sample/model::dep.safetensors" {
						t.Errorf("wrong dependency submission: %s", r.URL)
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id":3,"status":"waiting"}`))
					return
				}
				if uncertain {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
				} else {
					http.Error(w, "private source and token", http.StatusUnprocessableEntity)
				}
			})
			_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: starter}})
			submission, ok := errors.AsType[*models.StarterSubmissionError](err)
			if !ok || posts.Load() != 2 || len(submission.Progress.Jobs) != 1 || submission.Progress.Jobs[0].JobID != 3 || submission.Role != "starter" {
				t.Fatalf("error=%v posts=%d", err, posts.Load())
			}
			_, isUnknown := errors.AsType[*httpclient.OutcomeUnknownError](submission.Cause)
			if isUnknown != uncertain || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "sample/model") {
				t.Fatalf("error=%v unknown=%t", err, isUnknown)
			}
		})
	}
}
