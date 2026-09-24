package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

type queueClearServer struct {
	*httptest.Server
	requests atomic.Int32
	clears   atomic.Int32
}

// newQueueClearServer serves the version route and the 6.14.1 queue clear route
// for the default queue, answered by clearQueue. It counts every request and every
// clear request, and records any other request as an error.
func newQueueClearServer(t *testing.T, version string, clearQueue http.HandlerFunc) *queueClearServer {
	t.Helper()
	server := &queueClearServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.requests.Add(1)
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/app/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/api/v1/queue/default/clear" {
			server.clears.Add(1)
			clearQueue(w, r)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

// clearedCount answers a clear request as InvokeAI 6.14.1 does, with the number
// of queue items it deleted.
func clearedCount(deleted int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"deleted": deleted})
	}
}

func TestQueueClearDeletesQueueOnceAndReportsDeletedCount(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueClearServer(t, "6.14.1", clearedCount(3))

	exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "clear", "--yes", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	want := map[string]any{"queue_id": "default", "deleted": float64(3)}
	if envelope["operation"] != "queue.clear" || !reflect.DeepEqual(envelope["data"], want) {
		t.Fatalf("envelope = %#v, want data %#v", envelope, want)
	}
	if clears := server.clears.Load(); clears != 1 {
		t.Fatalf("sent %d clear requests, want 1", clears)
	}
}

func TestQueueClearRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "missing yes"},
		{name: "missing yes with queue id", args: []string{"--queue-id", "default"}},
		{name: "missing yes with request document", stdin: `{"schema_version":1,"queue_id":"default"}`, args: []string{"--request", "-"}},
		{name: "approval inside request document", stdin: `{"schema_version":1,"queue_id":"default","yes":true}`, args: []string{"--request", "-"}},
		{name: "positional argument", args: []string{"default", "--yes"}},
		{name: "queue id combined with request", stdin: `{"schema_version":1}`, args: []string{"--queue-id", "default", "--request", "-", "--yes"}},
		{name: "document with empty queue id", stdin: `{"schema_version":1,"queue_id":""}`, args: []string{"--request", "-", "--yes"}},
		{name: "document with item selector", stdin: `{"schema_version":1,"item_id":4}`, args: []string{"--request", "-", "--yes"}},
		{name: "document with unsupported schema version", stdin: `{"schema_version":2}`, args: []string{"--request", "-", "--yes"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newQueueClearServer(t, "6.14.1", clearedCount(3))
			args := append([]string{"queue", "clear", "--url", server.URL}, test.args...)

			exitCode, envelope, stderr := runBoardsJSON(t, test.stdin, args...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "queue.clear" || envelope["error"].(map[string]any)["code"] != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestQueueClearAcceptsTypedRequestDocumentWithApproval(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueClearServer(t, "6.14.1", clearedCount(0))

	exitCode, envelope, stderr := runBoardsJSON(t, `{"schema_version":1,"queue_id":"default"}`, "queue", "clear", "--request", "-", "--yes", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	want := map[string]any{"queue_id": "default", "deleted": float64(0)}
	if !reflect.DeepEqual(envelope["data"], want) || server.clears.Load() != 1 {
		t.Fatalf("envelope = %#v, clears = %d", envelope, server.clears.Load())
	}
}

func TestQueueClearSendsClearOnceOnEveryFailure(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name     string
		clear    http.HandlerFunc
		wantCode string
	}{
		{
			name: "conclusive rejection",
			clear: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"detail":"Unexpected error while clearing queue"}`, http.StatusInternalServerError)
			},
			wantCode: "invokeai_operation_failed",
		},
		{
			name: "transient gateway status",
			clear: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			},
			wantCode: "outcome_unknown",
		},
		{
			name: "lost response",
			clear: func(w http.ResponseWriter, _ *http.Request) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack clear connection: %v", err)
					return
				}
				_ = connection.Close()
			},
			wantCode: "outcome_unknown",
		},
		{
			name:     "undecodable success",
			clear:    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"deleted":`)) },
			wantCode: "outcome_unknown",
		},
		{
			name:     "success without a deleted count",
			clear:    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) },
			wantCode: "outcome_unknown",
		},
		{
			name:     "success with a negative deleted count",
			clear:    clearedCount(-1),
			wantCode: "outcome_unknown",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newQueueClearServer(t, "6.14.1", test.clear)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "clear", "--yes", "--url", server.URL)

			if exitCode != result.ExitInvokeAIFailure || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "queue.clear" || envelope["error"].(map[string]any)["code"] != test.wantCode {
				t.Fatalf("envelope = %#v, want code %q", envelope, test.wantCode)
			}
			if clears := server.clears.Load(); clears != 1 {
				t.Fatalf("sent %d clear requests, want exactly 1", clears)
			}
		})
	}
}

func TestQueueClearRejectsUnsupportedInvokeAIVersionBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	for _, version := range []string{"6.14.0", "6.15.0"} {
		t.Run(version, func(t *testing.T) {
			server := newQueueClearServer(t, version, clearedCount(3))

			exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "clear", "--yes", "--url", server.URL)

			if exitCode != result.ExitUnsupportedCapability || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["error"].(map[string]any)["code"] != "unsupported_capability" || server.clears.Load() != 0 {
				t.Fatalf("envelope = %#v, clears = %d", envelope, server.clears.Load())
			}
		})
	}
}

func TestQueueClearHumanOutputReportsDeletedCount(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueClearServer(t, "6.14.1", clearedCount(3))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "clear", "--yes", "--url", server.URL})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if want := "Deleted 3 queue items from queue default\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestQueueClearHelpStatesInvokeAIScope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "clear", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{"--yes", "admin", "every item in the", "only their own items", "neither widens nor narrows"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help does not state %q:\n%s", want, stdout.String())
		}
	}
}
