package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGetJSONSendsBearerTokenAndJoinsBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/api/v1/app/version" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "bediz/test" {
			t.Errorf("user-agent = %q", got)
		}
		if got := r.URL.Query().Get("detail"); got != "full" {
			t.Errorf("detail query = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
	}))
	defer server.Close()

	client, err := New(server.URL+"/proxy", "secret", Options{HTTPClient: server.Client(), UserAgent: "bediz/test"})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Version string `json:"version"`
	}
	if err := client.GetJSON(context.Background(), "/api/v1/app/version?detail=full", &response); err != nil {
		t.Fatal(err)
	}
	if response.Version != "6.14.1" {
		t.Fatalf("version = %q", response.Version)
	}
}

func TestGetRetriesTransientStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	client, err := New(server.URL, "", Options{HTTPClient: server.Client(), Retries: 2})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]bool
	if err := client.GetJSON(context.Background(), "/read", &response); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || !response["ok"] {
		t.Fatalf("calls = %d, response = %#v", calls.Load(), response)
	}
}

func TestMutationIsNeverRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := New(server.URL, "", Options{HTTPClient: server.Client(), Retries: 5})
	if err != nil {
		t.Fatal(err)
	}
	err = client.DoJSON(context.Background(), http.MethodPost, "/mutate", map[string]bool{"go": true}, nil)
	if err == nil {
		t.Fatal("expected request to fail")
	}
	if calls.Load() != 1 {
		t.Fatalf("mutation calls = %d, want 1", calls.Load())
	}
}

func TestMutationConnectionLossHasUnknownOutcome(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("connection reset")
	})
	client, err := New("https://invoke.example", "", Options{
		HTTPClient: &http.Client{Transport: transport},
		Retries:    5,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.DoJSON(context.Background(), http.MethodPost, "/mutate", map[string]bool{"go": true}, nil)
	var unknown *OutcomeUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %T %v, want OutcomeUnknownError", err, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("mutation calls = %d, want 1", calls.Load())
	}
}

func TestAuthenticationFailureIsClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := New(server.URL, "bad", Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = client.GetJSON(context.Background(), "/private", nil)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || !httpErr.AuthenticationFailure() {
		t.Fatalf("error = %#v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
