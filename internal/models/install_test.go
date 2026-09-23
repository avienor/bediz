package models_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
)

const huggingFaceInstallOpenAPI = `{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}},"/api/v2/models/hugging_face":{"get":{}}}}`
const pathInstallOpenAPI = `{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"inplace","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`

func installClient(t *testing.T, handler http.HandlerFunc) *httpclient.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestInstallCivitaiExactVersionUsesVerifiedFileURL(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != "https://civitai.com/api/download/models/42?fileId=7" {
				t.Errorf("unexpected install source: %s", r.URL)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":9,"status":"waiting"}`))
		default:
			t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL)
		}
	})
	metadata := &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != "https://civitai.com/api/v1/model-versions/42" {
			t.Errorf("unexpected Civitai request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":42,"files":[{"id":7,"name":"model.safetensors","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"}]}`)), Header: make(http.Header)}, nil
	})}
	got, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42"}})
	if err != nil || posts.Load() != 1 || len(got.Jobs) != 1 || got.Jobs[0].JobID != 9 || got.Jobs[0].SourceType != "civitai" {
		t.Fatalf("result=%#v err=%v posts=%d", got, err, posts.Load())
	}
}

func TestInstallCivitaiPageVersionSelectsOnlyPrimaryFile(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("source") != "https://civitai.com/api/download/models/42?fileId=8" {
				t.Errorf("source = %q", r.URL.Query().Get("source"))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":10,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	})
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":42,"modelId":21,"files":[{"id":7,"name":"preview.png","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"},{"id":8,"name":"model.safetensors","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=8"}]}`)), Header: make(http.Header)}, nil
	})}
	got, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "https://civitai.com/models/21?modelVersionId=42"}})
	if err != nil || posts.Load() != 1 || len(got.Jobs) != 1 || got.Jobs[0].JobID != 10 {
		t.Fatalf("result=%#v err=%v posts=%d", got, err, posts.Load())
	}
}

func TestInstallCivitaiAmbiguousFilesReturnNumericChoicesWithoutMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
	})
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":42,"files":[{"id":10,"name":"ten","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=10"},{"id":2,"name":"two","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=2"}]}`)), Header: make(http.Header)}, nil
	})}
	_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42"}})
	selection, ok := errors.AsType[*operation.CivitaiFileSelectionError](err)
	if !ok || selection.VersionID != 42 || !slices.Equal(selection.Candidates, []operation.CivitaiFileCandidate{{ID: 2, Name: "two", Primary: false}, {ID: 10, Name: "ten", Primary: false}}) || posts.Load() != 0 {
		t.Fatalf("selection=%#v err=%v posts=%d", selection, err, posts.Load())
	}
}

func TestInstallCivitaiModelPageReturnsNumericVersionChoicesWithoutMutation(t *testing.T) {
	for _, reference := range []string{"https://civitai.com/models/21", "https://civitai.com/models/21/sample-model"} {
		t.Run(reference, func(t *testing.T) {
			var requests atomic.Int32
			client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
			})
			metadata := &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.String() != "https://civitai.com/api/v1/models/21" {
					t.Errorf("unexpected Civitai request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":21,"name":"Sample","modelVersions":[{"id":300,"name":"v3.0"},{"id":42,"name":"v1.0"},{"id":100,"name":"v2.0"}]}`)), Header: make(http.Header)}, nil
			})}
			_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: reference}})
			selection, ok := errors.AsType[*operation.CivitaiVersionSelectionError](err)
			if !ok || selection.ModelID != 21 || !slices.Equal(selection.Candidates, []operation.CivitaiVersionCandidate{{ID: 42, Name: "v1.0"}, {ID: 100, Name: "v2.0"}, {ID: 300, Name: "v3.0"}}) || requests.Load() != 0 {
				t.Fatalf("selection=%#v err=%v requests=%d", selection, err, requests.Load())
			}
		})
	}
}

func TestInstallCivitaiSingleVersionModelPageStillRequiresVersionChoice(t *testing.T) {
	const token = "civitai-token-sentinel-742"
	var requests atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
	})
	metadata := &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("Civitai model metadata did not receive the source token")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":21,"modelVersions":[{"id":42,"name":"only","files":[{"id":7,"name":"model.safetensors","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"}]}]}`)), Header: make(http.Header)}, nil
	})}
	_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "https://civitai.com/models/21"}, SourceToken: token})
	selection, ok := errors.AsType[*operation.CivitaiVersionSelectionError](err)
	if !ok || selection.ModelID != 21 || !slices.Equal(selection.Candidates, []operation.CivitaiVersionCandidate{{ID: 42, Name: "only"}}) || requests.Load() != 0 || strings.Contains(err.Error(), token) {
		t.Fatalf("selection=%#v err=%v requests=%d", selection, err, requests.Load())
	}
}

func TestInstallCivitaiModelPageRejectsFileIDBeforeMetadata(t *testing.T) {
	var metadataRequests, requests atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
	})
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		metadataRequests.Add(1)
		return nil, errors.New("unexpected metadata request")
	})}
	_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "https://civitai.com/models/21", FileID: new(7)}})
	if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok || metadataRequests.Load() != 0 || requests.Load() != 0 {
		t.Fatalf("err=%v metadata=%d requests=%d", err, metadataRequests.Load(), requests.Load())
	}
}

func TestInstallCivitaiModelPageRejectsMalformedMetadataBeforeMutation(t *testing.T) {
	for _, body := range []string{
		`not-json`,
		`{"modelVersions":[{"id":42,"name":"v1"}]}`,
		`{"id":22,"modelVersions":[{"id":42,"name":"v1"}]}`,
		`{"id":21,"modelVersions":[]}`,
		`{"id":21}`,
		`{"id":21,"modelVersions":[{"name":"v1"}]}`,
		`{"id":21,"modelVersions":[{"id":0,"name":"v1"}]}`,
		`{"id":21,"modelVersions":[{"id":"42","name":"v1"}]}`,
		`{"id":21,"modelVersions":[{"id":42}]}`,
		`{"id":21,"modelVersions":[{"id":42,"name":"v1"},{"id":42,"name":"copy"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			var requests atomic.Int32
			client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
			})
			metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "https://civitai.com/models/21"}})
			if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok || requests.Load() != 0 {
				t.Fatalf("err=%v requests=%d", err, requests.Load())
			}
		})
	}
}

func TestInstallCivitaiMultiplePrimariesRequireFileChoice(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
	})
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":42,"files":[{"id":10,"name":"ten","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=10"},{"id":2,"name":"two","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=2"}]}`)), Header: make(http.Header)}, nil
	})}
	_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42"}})
	selection, ok := errors.AsType[*operation.CivitaiFileSelectionError](err)
	if !ok || len(selection.Candidates) != 2 || selection.Candidates[0].ID != 2 || !selection.Candidates[0].Primary || posts.Load() != 0 {
		t.Fatalf("selection=%#v err=%v posts=%d", selection, err, posts.Load())
	}
}

func TestInstallCivitaiSelectedFileMustBelongToExactVersion(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("source") != "https://civitai.com/api/download/models/42?fileId=2" {
				t.Errorf("source = %q", r.URL.Query().Get("source"))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":11,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	})
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":42,"files":[{"id":10,"name":"ten","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=10"},{"id":2,"name":"two","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=2"}]}`)), Header: make(http.Header)}, nil
	})}
	installer := models.Installer{Backend: client, CivitaiMetadataClient: metadata}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42", FileID: new(99)}})
	if err == nil || posts.Load() != 0 {
		t.Fatalf("out-of-version selection err=%v posts=%d", err, posts.Load())
	}
	got, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42", FileID: new(2)}})
	if err != nil || posts.Load() != 1 || len(got.Jobs) != 1 || got.Jobs[0].JobID != 11 {
		t.Fatalf("result=%#v err=%v posts=%d", got, err, posts.Load())
	}
}

func TestInstallCivitaiProtectedVersionUsesTemporaryTokenWithoutEcho(t *testing.T) {
	const token = "civitai-token-sentinel-742"
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("access_token") != token || r.URL.Query().Get("source") != "https://civitai.com/api/download/models/42?fileId=7" {
				t.Errorf("incorrect protected installation parameters")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":13,"status":"waiting","error":"civitai-token-sentinel-742"}`))
		default:
			t.Errorf("unexpected InvokeAI request: %s", r.URL.Path)
		}
	})
	metadata := &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("Civitai metadata request lacks temporary bearer token")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":42,"files":[{"id":7,"name":"model.safetensors","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"}]}`)), Header: make(http.Header)}, nil
	})}
	got, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42"}, SourceToken: token})
	if err != nil || posts.Load() != 1 {
		t.Fatalf("result=%#v err=%v posts=%d", got, err, posts.Load())
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), token) {
		t.Fatalf("token leaked in result: %s", encoded)
	}
}

func TestInstallCivitaiRejectsInvalidVersionReferencesBeforeMetadata(t *testing.T) {
	for _, reference := range []string{
		"0", "-1", "+42", "4.2", "abc", "https://civitai.com/models/0", "https://civitai.com/models/12?",
		"https://civitai.com/models/12?token=secret", "https://civitai.com/models/12#fragment",
		"https://civitai.com/models/12?modelVersionId=0",
		"https://civitai.com/models/12?modelVersionId=42&modelVersionId=43",
		"https://civitai.com/models/12?modelVersionId=42&token=secret",
		"https://civitai.com/models/12?modelVersionId=42#fragment",
		"https://user:secret@civitai.com/models/12?modelVersionId=42",
		"https://elsewhere.example/models/12?modelVersionId=42",
		"https://civitai.com/api/download/models/42?fileId=7",
	} {
		t.Run(reference, func(t *testing.T) {
			var metadataRequests, posts atomic.Int32
			client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
			})
			metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
				metadataRequests.Add(1)
				return nil, errors.New("unexpected metadata request")
			})}
			_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: reference}})
			if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok || metadataRequests.Load() != 0 || posts.Load() != 0 {
				t.Fatalf("err=%v metadata=%d posts=%d", err, metadataRequests.Load(), posts.Load())
			}
		})
	}
}

func TestInstallCivitaiRejectsMalformedOrUnsafeMetadataBeforeMutation(t *testing.T) {
	for _, body := range []string{
		`not-json`,
		`{"id":43,"files":[{"id":7,"name":"file","primary":true,"downloadUrl":"https://civitai.com/api/download/models/43?fileId=7"}]}`,
		`{"id":42,"files":[]}`,
		`{"id":42,"files":[{"id":7,"name":"file","downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"}]}`,
		`{"id":42,"files":[{"id":7,"name":"file","primary":true,"downloadUrl":"https://evil.example/file"}]}`,
		`{"id":42,"files":[{"id":7,"name":"file","primary":true,"downloadUrl":"https://user:secret@civitai.com/api/download/models/42?fileId=7"}]}`,
		`{"id":42,"files":[{"id":7,"name":"file","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7&token=secret"}]}`,
		`{"id":42,"files":[{"id":7,"name":"file","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7#fragment"}]}`,
		`{"id":42,"downloadUrl":"https://evil.example/download","files":[{"id":7,"name":"file","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"}]}`,
		`{"id":42,"files":[{"id":7,"name":"file","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"},{"id":7,"name":"copy","primary":false,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			var posts atomic.Int32
			client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
			})
			metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42"}})
			if err == nil || posts.Load() != 0 || strings.Contains(err.Error(), "evil.example") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err=%v posts=%d", err, posts.Load())
			}
		})
	}
}

func TestInstallCivitaiMetadataDenialIsSafeAuthenticationFailure(t *testing.T) {
	const token = "civitai-token-sentinel-742"
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
	})
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(token)), Header: make(http.Header)}, nil
	})}
	_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42"}, SourceToken: token})
	if _, ok := errors.AsType[*operation.AuthenticationRequiredError](err); !ok || posts.Load() != 0 || strings.Contains(err.Error(), token) {
		t.Fatalf("err=%v posts=%d", err, posts.Load())
	}
}

func TestInstallCivitaiPageRejectsVersionFromAnotherModel(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		http.Error(w, "unexpected InvokeAI request", http.StatusInternalServerError)
	})
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":42,"modelId":99,"files":[{"id":7,"name":"model.safetensors","primary":true,"downloadUrl":"https://civitai.com/api/download/models/42?fileId=7"}]}`)), Header: make(http.Header)}, nil
	})}
	_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "https://civitai.com/models/21?modelVersionId=42"}})
	if err == nil || posts.Load() != 0 {
		t.Fatalf("err=%v posts=%d", err, posts.Load())
	}
}

func TestInstallCivitaiTokenRejectsInsecureTargetBeforeMetadata(t *testing.T) {
	client, err := httpclient.New("http://example.org", "", httpclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var metadataRequests atomic.Int32
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		metadataRequests.Add(1)
		return nil, errors.New("unexpected metadata request")
	})}
	_, err = (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42"}, SourceToken: "sentinel"})
	if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok || metadataRequests.Load() != 0 {
		t.Fatalf("err=%v metadata=%d", err, metadataRequests.Load())
	}
}

func TestInstallCivitaiMetadataCancellationRemainsInterrupted(t *testing.T) {
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected InvokeAI request")
	})
	metadata := &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})}
	_, err := (models.Installer{Backend: client, CivitaiMetadataClient: metadata}).Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "civitai", Reference: "42"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want cancellation", err)
	}
}

func TestInstallURLSubmitsOneGenericPOSTAndProjectsJob(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != "https://example.org/model.safetensors" {
				t.Errorf("unexpected mutation: %s %s", r.Method, r.URL)
			}
			var body map[string]any
			if err := json.UnmarshalRead(r.Body, &body); err != nil {
				t.Error(err)
			}
			if len(body) != 0 {
				t.Errorf("body = %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":0,"status":"waiting","source":{"type":"url","url":"https://example.org/model.safetensors","access_token":"secret"},"error":"private"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	got, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "url", Reference: "https://example.org/model.safetensors"}})
	if err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 || len(got.Jobs) != 1 || got.Jobs[0].JobID != 0 || got.Jobs[0].Status != "waiting" || got.Jobs[0].SourceType != "url" || got.Jobs[0].Role != "requested" {
		t.Fatalf("result = %#v; posts = %d", got, posts.Load())
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "example.org") || strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private") {
		t.Fatalf("leaked result %s", encoded)
	}
}

func TestInstallPathRegistersInPlaceThroughServerNamespace(t *testing.T) {
	const source = "/invokeai-server/fixtures/model.safetensors"
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(pathInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != source || r.URL.Query().Get("inplace") != "true" {
				t.Errorf("unexpected path installation: %s %s", r.Method, r.URL)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":17,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	got, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "path", Reference: source}})
	if err != nil || posts.Load() != 1 || len(got.Jobs) != 1 || got.Jobs[0].JobID != 17 || got.Jobs[0].SourceType != "path" {
		t.Fatalf("result=%#v err=%v posts=%d", got, err, posts.Load())
	}
}

func TestInstallPathMoveNeedsApprovalBeforeNetworkAndSubmitsOnce(t *testing.T) {
	const source = "/invokeai-server/fixtures/move-me.safetensors"
	var requests, posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(pathInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("source") != source || r.URL.Query().Get("inplace") != "false" {
				t.Errorf("unexpected move: %s", r.URL)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":18,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	request := models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "path", Reference: source}, Move: new(true)}
	_, err := models.Install(t.Context(), client, request)
	if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok || requests.Load() != 0 {
		t.Fatalf("unapproved move: err=%v requests=%d", err, requests.Load())
	}
	request.Approved = true
	got, err := models.Install(t.Context(), client, request)
	if err != nil || posts.Load() != 1 || len(got.Jobs) != 1 || got.Jobs[0].JobID != 18 {
		t.Fatalf("approved move: result=%#v err=%v posts=%d", got, err, posts.Load())
	}
}

func TestInstallPathRejectsAmbiguousRelativeReferenceBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	})
	_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "path", Reference: "org/repo"}})
	if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok || requests.Load() != 0 {
		t.Fatalf("err=%v requests=%d", err, requests.Load())
	}
}

func TestInstallPathRequiresTestedInplaceParameterBeforeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
		case "/api/v2/models/install":
			posts.Add(1)
		}
	})
	_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "path", Reference: "/server/model.safetensors"}})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
		t.Fatalf("err=%v posts=%d", err, posts.Load())
	}
}

func TestInstallHuggingFaceIDUsesCanonicalRepositoryURLAndInspectableJob(t *testing.T) {
	workingDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workingDir, "sample", "model"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workingDir)
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			if r.Method != http.MethodGet || r.URL.Query().Get("hugging_face_repo") != "sample/model" {
				t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
			}
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/model.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != "https://huggingface.co/sample/model" {
				t.Errorf("unexpected mutation: %s %s", r.Method, r.URL)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://huggingface.co/api/models/sample/model" {
			t.Errorf("unexpected public repository check: %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	got, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model"}})
	if err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 || len(got.Jobs) != 1 || got.Jobs[0].JobID != 7 || got.Jobs[0].SourceType != "huggingface" {
		t.Fatalf("result=%#v posts=%d", got, posts.Load())
	}
}

func TestStarterRepositoryDependencyUsesCanonicalURLDespiteLocalPathCollision(t *testing.T) {
	workingDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workingDir, "sample", "dep"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workingDir)
	var submitted []string
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}},"/api/v2/models/starter_models":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/StarterModelResponse"}}}}}}},"/api/v2/models/hugging_face":{"get":{}}}}`))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(`{"starter_models":[{"source":"https://example.org/main.safetensors","is_installed":false,"dependencies":[{"source":"sample/dep","is_installed":false}]}],"starter_bundles":{}}`))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/dep/resolve/main/dep.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			submitted = append(submitted, r.URL.Query().Get("source"))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://huggingface.co/api/models/sample/dep" {
			t.Errorf("unexpected public repository check: %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	got, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: "https://example.org/main.safetensors"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(submitted, []string{"https://huggingface.co/sample/dep", "https://example.org/main.safetensors"}) || len(got.Jobs) != 2 {
		t.Fatalf("submitted=%q jobs=%#v", submitted, got.Jobs)
	}
}

func TestProtectedStarterDependencyNeedsBothAuthenticationInputsBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name  string
		token string
		login string
	}{
		{name: "missing download token"},
		{name: "invalid InvokeAI login", token: "download-secret", login: "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var posts atomic.Int32
			client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}},"/api/v2/models/starter_models":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/StarterModelResponse"}}}}}}},"/api/v2/models/hugging_face":{"get":{}}}}`))
				case "/api/v2/models/starter_models":
					_, _ = w.Write([]byte(`{"starter_models":[{"source":"https://example.org/main","is_installed":false,"dependencies":[{"source":"sample/private","is_installed":false}]}],"starter_bundles":{}}`))
				case "/api/v2/models/hf_login":
					_, _ = w.Write([]byte(`"` + test.login + `"`))
				case "/api/v2/models/install":
					posts.Add(1)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
			})
			installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}, nil
			})}}
			_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: "https://example.org/main"}, SourceToken: test.token})
			if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 || strings.Contains(err.Error(), test.token) && test.token != "" {
				t.Fatalf("error=%v posts=%d", err, posts.Load())
			}
		})
	}
}

func TestProtectedStarterArtifactDependencyNeedsDownloadTokenBeforeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}},"/api/v2/models/starter_models":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/StarterModelResponse"}}}}}}},"/api/v2/models/hugging_face":{"get":{}}}}`))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(`{"starter_models":[{"source":"https://huggingface.co/sample/main/resolve/main/main.safetensors","is_installed":false,"dependencies":[{"source":"https://example.org/public.safetensors","is_installed":false},{"source":"https://huggingface.co/sample/gated/resolve/main/dep.safetensors","is_installed":false}]}],"starter_bundles":{}}`))
		case "/api/v2/models/install":
			posts.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "https://huggingface.co/api/models/sample/gated" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":"manual","private":false}`)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: "https://huggingface.co/sample/main/resolve/main/main.safetensors"}})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestStarterRepositoryWithoutOneSupportedArtifactFailsBeforeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}},"/api/v2/models/starter_models":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/StarterModelResponse"}}}}}}},"/api/v2/models/hugging_face":{"get":{}}}}`))
		case "/api/v2/models/starter_models":
			_, _ = w.Write([]byte(`{"starter_models":[{"source":"sample/empty","is_installed":false}],"starter_bundles":{}}`))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":[],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "starter", Reference: "sample/empty"}})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestHuggingFaceInstallRejectsVariantSubfolderAndUnsafeReferencesBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	client := installClient(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })
	for _, reference := range []string{
		"sample/model:fp16", "sample/model::vae", "sample/model/resolve/main/file.safetensors",
		"https://huggingface.co/sample/model/tree/main", "https://huggingface.co/sample/model?token=secret",
		"http://huggingface.co/sample/model", "https://other.example/sample/model", "sample/model#fragment",
	} {
		_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: reference}})
		if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok {
			t.Errorf("reference %q: error=%v", reference, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("network requests=%d", requests.Load())
	}
}

func TestProtectedHuggingFaceInstallRequiresDownloadTokenBeforeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
		default:
			t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != "https://huggingface.co/api/models/sample/private" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected anonymous repository check: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":"manual","private":false}`)), Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/private"}})
	if _, ok := errors.AsType[*operation.AuthenticationRequiredError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestProtectedHuggingFaceInstallRequiresValidInvokeAILogin(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hf_login":
			_, _ = w.Write([]byte(`"invalid"`))
		case "/api/v2/models/install":
			posts.Add(1)
		default:
			t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/private"}, SourceToken: "download-secret"})
	if _, ok := errors.AsType[*operation.AuthenticationRequiredError](err); !ok || posts.Load() != 0 || strings.Contains(err.Error(), "download-secret") {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestProtectedHuggingFaceInstallWithBothCredentialsReturnsJob(t *testing.T) {
	const token = "download-secret"
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hf_login":
			_, _ = w.Write([]byte(`"valid"`))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/private/resolve/main/model.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("source") != "https://huggingface.co/sample/private" || r.URL.Query().Get("access_token") != token {
				t.Error("protected repository was not submitted with its canonical source and download token")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":11,"status":"waiting","source":{"access_token":"download-secret"}}`))
		default:
			t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}, nil
	})}}
	got, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "https://huggingface.co/sample/private"}, SourceToken: token})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got)
	if posts.Load() != 1 || len(got.Jobs) != 1 || got.Jobs[0].JobID != 11 || got.Jobs[0].SourceType != "huggingface" || strings.Contains(string(encoded), token) {
		t.Fatalf("result=%s posts=%d", encoded, posts.Load())
	}
}

func TestHuggingFaceRepositoryWithMultipleArtifactsRequiresSelection(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/a.safetensors","https://huggingface.co/sample/model/resolve/main/b.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model"}})
	selection, ok := errors.AsType[*operation.SelectionRequiredError](err)
	if !ok || posts.Load() != 0 || len(selection.Candidates) != 2 || selection.Candidates[0].Key != "https://huggingface.co/sample/model/resolve/main/a.safetensors" {
		t.Fatalf("selection=%#v error=%v posts=%d", selection, err, posts.Load())
	}
}

func TestHuggingFaceArtifactSelectionSubmitsExactCandidate(t *testing.T) {
	const artifact = "https://huggingface.co/sample/model/resolve/main/b.safetensors"
	var submitted []string
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/a.safetensors","` + artifact + `"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			submitted = append(submitted, r.URL.Query().Get("source"))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":12,"status":"waiting"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	got, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model", Artifact: artifact}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(submitted, []string{artifact}) || len(got.Jobs) != 1 || got.Jobs[0].JobID != 12 || got.Jobs[0].SourceType != "huggingface" {
		t.Fatalf("submitted=%q result=%#v", submitted, got)
	}
}

func TestHuggingFaceArtifactSelectionRejectsNonCandidateBeforeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/a.safetensors","https://huggingface.co/sample/model/resolve/main/b.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	for _, artifact := range []string{
		"https://huggingface.co/sample/model/resolve/main/c.safetensors",
		"https://huggingface.co/other/model/resolve/main/a.safetensors",
		"https://example.org/a.safetensors",
	} {
		_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model", Artifact: artifact}})
		if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok {
			t.Errorf("artifact %q: error=%v", artifact, err)
		}
	}
	if posts.Load() != 0 {
		t.Fatalf("posts=%d", posts.Load())
	}
}

func TestProtectedHuggingFaceArtifactSelectionRequiresValidInvokeAILogin(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hf_login":
			_, _ = w.Write([]byte(`"invalid"`))
		case "/api/v2/models/install":
			posts.Add(1)
		default:
			t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/private", Artifact: "https://huggingface.co/sample/private/resolve/main/a.safetensors"}, SourceToken: "download-secret"})
	if _, ok := errors.AsType[*operation.AuthenticationRequiredError](err); !ok || posts.Load() != 0 || strings.Contains(err.Error(), "download-secret") {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestInstallRejectsArtifactForNonHuggingFaceSourceBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	client := installClient(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })
	for _, source := range []models.InstallSource{
		{Type: "url", Reference: "https://example.org/model.safetensors"},
		{Type: "starter", Reference: "https://example.org/model.safetensors"},
		{Type: "path", Reference: "/models/model.safetensors"},
		{Type: "civitai", Reference: "42"},
	} {
		source.Artifact = "https://huggingface.co/sample/model/resolve/main/a.safetensors"
		_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: source})
		if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok {
			t.Errorf("source type %q: error=%v", source.Type, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("network requests=%d", requests.Load())
	}
}

func TestHuggingFaceInstallRejectsUnsafeSingleArtifactBeforeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://other.example/model.safetensors?token=private"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model"}})
	if _, ok := errors.AsType[*httpclient.InvalidResponseError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestHuggingFaceInstallLostResponseIsUnknownWithoutResubmission(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/model.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model"}})
	if _, ok := errors.AsType[*httpclient.OutcomeUnknownError](err); !ok || posts.Load() != 1 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestHuggingFaceInstallDoesNotReplayPOSTAfterRedirect(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/hugging_face":
			_, _ = w.Write([]byte(`{"urls":["https://huggingface.co/sample/model/resolve/main/model.safetensors"],"is_diffusers":false}`))
		case "/api/v2/models/install":
			posts.Add(1)
			w.Header().Set("Location", "/api/v2/models/install/replayed")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/api/v2/models/install/replayed":
			posts.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":91,"status":"waiting"}`))
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":false,"private":false}`)), Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model"}})
	httpErr, ok := errors.AsType[*httpclient.HTTPError](err)
	if !ok || httpErr.StatusCode != http.StatusTemporaryRedirect || posts.Load() != 1 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestHuggingFaceAccessCheckFailureStopsBeforeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model"}})
	if _, ok := errors.AsType[*models.RepositoryAccessError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestHuggingFaceIncompleteAccessMetadataCannotAuthorizeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(huggingFaceInstallOpenAPI))
		case "/api/v2/models/install":
			posts.Add(1)
		default:
			t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"gated":"unexpected","private":false}`)), Header: make(http.Header)}, nil
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model"}, SourceToken: "download-secret"})
	if _, ok := errors.AsType[*models.RepositoryAccessError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestHuggingFaceInstallRequiresReadOnlyInvokeAIMetadataEndpoint(t *testing.T) {
	var requests atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	installer := models.Installer{Backend: client, PublicRepositoryClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("public access check occurred without the tested InvokeAI metadata endpoint")
		return nil, errors.New("unexpected request")
	})}}
	_, err := installer.Install(t.Context(), models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "huggingface", Reference: "sample/model"}})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || requests.Load() != 2 {
		t.Fatalf("error=%v requests=%d", err, requests.Load())
	}
}

func TestInstallURLRejectsUnsafeSourcesBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	client := installClient(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })
	for _, source := range []string{"https://user:pass@example.org/model", "https://example.org/model?token=x", "https://example.org/model#part", "https://example.org/model#", "ftp://example.org/model", "//example.org/model", "http://example.org/%zz"} {
		_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "url", Reference: source}})
		if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok {
			t.Errorf("source %q: error = %v", source, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("network requests = %d", requests.Load())
	}
}

func TestInstallRejectsGenericPOSTWithoutInspectableJobResponse(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}]}}}}`))
		case "/api/v2/models/install":
			posts.Add(1)
		}
	})
	_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "url", Reference: "https://example.org/model.safetensors"}})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestStatusProjectsExactCurrentJobWithoutPrivateFields(t *testing.T) {
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/models/install/0" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"id":0,"status":"completed","bytes":100,"total_bytes":100,"config_out":{"key":"installed-key"},"source":{"access_token":"secret","url":"https://private.example/model"},"error":"private-error","error_traceback":"private-traceback"}`))
	})
	got, err := models.Status(t.Context(), client, models.StatusRequest{SchemaVersion: 1, JobID: new(0)})
	if err != nil {
		t.Fatal(err)
	}
	if got.JobID != 0 || got.Status != "completed" || got.ModelKey != "installed-key" || got.Bytes == nil || *got.Bytes != 100 {
		t.Fatalf("status = %#v", got)
	}
	encoded, _ := json.Marshal(got)
	for _, secret := range []string{"secret", "private", "traceback", "source"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("leaked %s: %s", secret, encoded)
		}
	}
}

func TestStatusDoesNotClaimCompletedModelKeyForOtherStates(t *testing.T) {
	client := installClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":12,"status":"error","config_out":{"key":"stale-key"},"error":"private"}`))
	})
	got, err := models.Status(t.Context(), client, models.StatusRequest{SchemaVersion: 1, JobID: new(12)})
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelKey != "" || got.Status != "error" {
		t.Fatalf("status = %#v", got)
	}
}

func TestStatusAcceptsEveryTestedInvokeAIInstallState(t *testing.T) {
	for _, state := range []string{"waiting", "downloading", "downloads_done", "running", "paused", "completed", "error", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			client := installClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":3,"status":"` + state + `"}`))
			})
			got, err := models.Status(t.Context(), client, models.StatusRequest{SchemaVersion: 1, JobID: new(3)})
			if err != nil || got.Status != state {
				t.Fatalf("status=%#v err=%v", got, err)
			}
		})
	}
}

func TestStatusAlwaysReadsCurrentRegistryEntryAfterIDReuse(t *testing.T) {
	var calls atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"id":2,"status":"completed","config_out":{"key":"old-key"}}`))
		} else {
			_, _ = w.Write([]byte(`{"id":2,"status":"waiting","source":{"access_token":"private"}}`))
		}
	})
	first, err := models.Status(t.Context(), client, models.StatusRequest{SchemaVersion: 1, JobID: new(2)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := models.Status(t.Context(), client, models.StatusRequest{SchemaVersion: 1, JobID: new(2)})
	if err != nil {
		t.Fatal(err)
	}
	if first.ModelKey != "old-key" || second.Status != "waiting" || second.ModelKey != "" || calls.Load() != 2 {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestStatusRejectsReusedOrUnknownJobIdentity(t *testing.T) {
	for _, payload := range []string{`{"id":9,"status":"completed","config_out":{"key":"old-key"}}`, `{"id":4,"status":"invented"}`} {
		client := installClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) })
		_, err := models.Status(t.Context(), client, models.StatusRequest{SchemaVersion: 1, JobID: new(4)})
		if _, ok := errors.AsType[*httpclient.InvalidResponseError](err); !ok {
			t.Errorf("payload %s: %v", payload, err)
		}
	}
}

func TestStatusMissingJobReturnsNotFoundWithoutGuessing(t *testing.T) {
	client := installClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	_, err := models.Status(t.Context(), client, models.StatusRequest{SchemaVersion: 1, JobID: new(4)})
	if httpErr, ok := errors.AsType[*httpclient.HTTPError](err); !ok || httpErr.StatusCode != 404 {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallLostResponseReturnsUnknownAfterOnePOST(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
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
	})
	_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "url", Reference: "https://example.org/model"}})
	if _, ok := errors.AsType[*httpclient.OutcomeUnknownError](err); !ok || posts.Load() != 1 {
		t.Fatalf("error = %v; posts = %d", err, posts.Load())
	}
}

func TestInstallUntestedVersionRejectsBeforeMutation(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		_, _ = w.Write([]byte(`{"version":"6.15.0"}`))
	})
	_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "url", Reference: "https://example.org/model"}})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error = %v; posts = %d", err, posts.Load())
	}
}

type installTransportFunc func(*http.Request) (*http.Response, error)

func (f installTransportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestInstallProtectedURLRejectsNonLoopbackPlainHTTPBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	client, err := httpclient.New("http://invoke.example", "", httpclient.Options{HTTPClient: &http.Client{Transport: installTransportFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected request")
	})}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "url", Reference: "https://example.org/model"}, SourceToken: "source-token-sentinel-742"})
	if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok || requests.Load() != 0 || strings.Contains(err.Error(), "source-token-sentinel-742") {
		t.Fatalf("error=%v requests=%d", err, requests.Load())
	}
}

func TestInstallProtectedURLRedactsBackendFailureFromPublicError(t *testing.T) {
	const token = "source-token-sentinel-742"
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true},{"name":"access_token","in":"query"}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
		case "/api/v2/models/install":
			posts.Add(1)
			if r.URL.Query().Get("access_token") != token {
				t.Error("source token missing from native parameter")
			}
			http.Error(w, "rejected "+token, http.StatusUnauthorized)
		}
	})
	_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "url", Reference: "https://example.org/model"}, SourceToken: token})
	if httpErr, ok := errors.AsType[*httpclient.HTTPError](err); !ok || httpErr.StatusCode != http.StatusUnauthorized || httpErr.Body != "" || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "access_token") || posts.Load() != 1 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}

func TestInstallProtectedURLRequiresAdvertisedAccessTokenParameter(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
		case "/api/v2/models/install":
			posts.Add(1)
		}
	})
	_, err := models.Install(t.Context(), client, models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: "url", Reference: "https://example.org/model"}, SourceToken: "source-token-sentinel-742"})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts.Load() != 0 {
		t.Fatalf("error=%v posts=%d", err, posts.Load())
	}
}
