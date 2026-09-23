package models_test

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
)

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

func TestInstallURLSubmitsOneGenericPOSTAndProjectsJob(t *testing.T) {
	var posts atomic.Int32
	client := installClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}]}}}}`))
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
