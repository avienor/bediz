package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	var roundTrips atomic.Int32
	ctx := newCancelWhenWaitedContext()
	client, err := New("https://invoke.example", "", Options{
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			roundTrips.Add(1)
			ctx.arm()
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Body:       io.NopCloser(strings.NewReader("try again")),
			}, nil
		})},
		Retries: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.GetJSON(ctx, "/read", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %#v, want context.Canceled", err)
	}
	if roundTrips.Load() != 1 {
		t.Fatalf("transport attempts = %d, want 1", roundTrips.Load())
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

func TestNewResolvesOptionFallbacks(t *testing.T) {
	var userAgent atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent.Store(r.Header.Get("User-Agent"))
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	client, err := New(server.URL, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]bool
	if err := client.GetJSON(t.Context(), "/read", &response); err != nil {
		t.Fatal(err)
	}
	if !response["ok"] {
		t.Fatalf("response = %#v", response)
	}
	if got := userAgent.Load(); got != "bediz/dev" {
		t.Errorf("user-agent = %q, want bediz/dev", got)
	}
}

func TestNewDoesNotEnableRetriesForCustomHTTPClient(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := New(server.URL, "", Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = client.GetJSON(t.Context(), "/read", nil)
	if httpErr, ok := errors.AsType[*HTTPError](err); !ok || httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("error = %#v, want conclusive service unavailable failure", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestNewAppliesConfiguredHTTPOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "bediz/custom" {
			t.Errorf("user-agent = %q, want bediz/custom", got)
		}
		_, _ = w.Write(bytes.Repeat([]byte("x"), 64))
	}))
	defer server.Close()
	client, err := New(server.URL, "", Options{HTTPClient: server.Client(), MaxBody: 8, UserAgent: "bediz/custom"})
	if err != nil {
		t.Fatal(err)
	}

	err = client.GetJSON(t.Context(), "/read", nil)
	if _, ok := errors.AsType[*InvalidResponseError](err); !ok {
		t.Fatalf("error = %#v, want InvalidResponseError for a body above the configured maximum", err)
	}
}

func TestNewAppliesConfiguredTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()
	client, err := New(server.URL, "", Options{Timeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	err = client.DoJSON(t.Context(), http.MethodPost, "/mutate", map[string]bool{"go": true}, nil)
	if _, ok := errors.AsType[*OutcomeUnknownError](err); !ok {
		t.Fatalf("error = %#v, want OutcomeUnknownError for a request past the configured timeout", err)
	}
}

func TestNewRejectsNegativeOptions(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		want    string
	}{
		{name: "timeout", options: Options{Timeout: -time.Second}, want: "timeout must be positive"},
		{name: "retries", options: Options{Retries: -1}, want: "retries cannot be negative"},
		{name: "maximum response body", options: Options{MaxBody: -1}, want: "maximum response body size must be positive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New("http://127.0.0.1:9090", "", test.options)
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// cancelWhenWaitedContext becomes canceled when a caller observes Done after
// arm. The transport arms it after the first response, so retry waiting is the
// first operation that can trigger cancellation.
type cancelWhenWaitedContext struct {
	armed    atomic.Bool
	canceled atomic.Bool
	done     chan struct{}
}

func newCancelWhenWaitedContext() *cancelWhenWaitedContext {
	return &cancelWhenWaitedContext{done: make(chan struct{})}
}

func (c *cancelWhenWaitedContext) arm() {
	c.armed.Store(true)
}

func (c *cancelWhenWaitedContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *cancelWhenWaitedContext) Done() <-chan struct{} {
	if c.armed.Load() && c.canceled.CompareAndSwap(false, true) {
		close(c.done)
	}
	return c.done
}

func (c *cancelWhenWaitedContext) Err() error {
	if c.canceled.Load() {
		return context.Canceled
	}
	return nil
}

func (c *cancelWhenWaitedContext) Value(any) any {
	return nil
}
