package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
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
	if err := client.GetJSON(t.Context(), "/api/v1/app/version?detail=full", &response); err != nil {
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
	if err := client.GetJSON(t.Context(), "/read", &response); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || !response["ok"] {
		t.Fatalf("calls = %d, response = %#v", calls.Load(), response)
	}
}

func TestReadOnlyPostRetriesTransientStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	client, err := New(server.URL, "", Options{HTTPClient: server.Client(), Retries: 1})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]bool
	if err := client.QueryJSON(t.Context(), "/query", map[string]any{"ids": []int{1}}, &response); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !response["ok"] {
		t.Fatalf("calls = %d, response = %#v", calls.Load(), response)
	}
}

func TestGetRetriesExhaustConfiguredAttempts(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := New(server.URL, "", Options{HTTPClient: server.Client(), Retries: 2})
	if err != nil {
		t.Fatal(err)
	}
	err = client.GetJSON(t.Context(), "/read", nil)
	if httpErr, ok := errors.AsType[*HTTPError](err); !ok || httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("error = %#v, want conclusive service unavailable failure", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestContextCancellationStopsRetryAttempts(t *testing.T) {
	var serverCalls atomic.Int32
	var roundTrips atomic.Int32
	inFlight := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverCalls.Add(1) == 1 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		close(inFlight)
		<-r.Context().Done()
	}))
	defer server.Close()

	transport := server.Client().Transport
	client, err := New(server.URL, "", Options{
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			roundTrips.Add(1)
			return transport.RoundTrip(request)
		})},
		Retries: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results := make(chan error, 1)
	go func() { results <- client.GetJSON(ctx, "/read", nil) }()
	<-inFlight
	cancel()

	if err := <-results; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %#v, want context.Canceled", err)
	}
	if serverCalls.Load() != 2 || roundTrips.Load() != 2 {
		t.Fatalf("server calls = %d, transport attempts = %d, want 2 and 2", serverCalls.Load(), roundTrips.Load())
	}
}

func TestContextCancellationDuringRetryWaitStopsFurtherAttempts(t *testing.T) {
	var serverCalls atomic.Int32
	var roundTrips atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverCalls.Add(1)
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	transport := server.Client().Transport
	client, err := New(server.URL, "", Options{
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			roundTrips.Add(1)
			return transport.RoundTrip(request)
		})},
		Retries: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var waitInput time.Duration
	client.retryWait = func(waitCtx context.Context, duration time.Duration) error {
		waitInput = duration
		cancel()
		return wait(waitCtx, duration)
	}

	err = client.GetJSON(ctx, "/read", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %#v, want context.Canceled", err)
	}
	if serverCalls.Load() != 1 || roundTrips.Load() != 1 {
		t.Fatalf("server calls = %d, transport attempts = %d, want 1 and 1", serverCalls.Load(), roundTrips.Load())
	}
	if waitInput != 100*time.Millisecond {
		t.Fatalf("first retry wait = %s, want 100ms", waitInput)
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
	err = client.DoJSON(t.Context(), http.MethodPost, "/mutate", map[string]bool{"go": true}, nil)
	if httpErr, ok := errors.AsType[*HTTPError](err); !ok || httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("error = %#v, want conclusive service unavailable failure", err)
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
	err = client.DoJSON(t.Context(), http.MethodPost, "/mutate", map[string]bool{"go": true}, nil)
	if _, ok := errors.AsType[*OutcomeUnknownError](err); !ok {
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
	err = client.GetJSON(t.Context(), "/private", nil)
	if httpErr, ok := errors.AsType[*HTTPError](err); !ok || !httpErr.AuthenticationFailure() {
		t.Fatalf("error = %#v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
