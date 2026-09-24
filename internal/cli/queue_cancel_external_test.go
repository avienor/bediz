package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

type queueCancelServer struct {
	*httptest.Server
	requests atomic.Int32
	cancels  atomic.Int32
}

// newQueueCancelServer serves the version route, the 6.14.1 per-item cancel
// route answered by cancel, and read-only image inspection. It counts every
// request and every cancel request, and records any other request as an error.
func newQueueCancelServer(t *testing.T, version string, cancel func(w http.ResponseWriter, itemID int)) *queueCancelServer {
	t.Helper()
	return newQueueCancelServerWithImages(t, version, cancel, nil)
}

// newQueueCancelServerWithImages is newQueueCancelServer with image detail
// reads answered by image instead of a valid Image Reference when it is set.
func newQueueCancelServerWithImages(t *testing.T, version string, cancel func(w http.ResponseWriter, itemID int), image http.HandlerFunc) *queueCancelServer {
	t.Helper()
	server := &queueCancelServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.requests.Add(1)
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/app/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
			return
		}
		if value, ok := strings.CutPrefix(r.URL.Path, "/api/v1/queue/default/i/"); ok && r.Method == http.MethodPut {
			if value, ok := strings.CutSuffix(value, "/cancel"); ok {
				itemID, err := strconv.Atoi(value)
				if err != nil {
					t.Errorf("cancel item id %q is not an integer", value)
				}
				server.cancels.Add(1)
				cancel(w, itemID)
				return
			}
		}
		if name, ok := strings.CutPrefix(r.URL.Path, "/api/v1/images/i/"); ok && r.Method == http.MethodGet {
			if image != nil {
				image(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": name, "image_url": "api/v1/images/i/" + name + "/full",
				"thumbnail_url": "api/v1/images/i/" + name + "/thumbnail",
				"image_origin":  "internal", "image_category": "general", "width": 768, "height": 768,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false,
				"has_workflow": true,
			})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

// canceledItem answers a cancel request with the named item in status, as
// InvokeAI 6.14.1 returns it after the cancellation.
func canceledItem(status string, extra map[string]any) func(http.ResponseWriter, int) {
	return func(w http.ResponseWriter, itemID int) {
		_ = json.NewEncoder(w).Encode(waitItemPayload(itemID, status, extra))
	}
}

func TestQueueCancelCancelsPendingItemOnce(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueCancelServer(t, "6.14.1", canceledItem("canceled", nil))

	exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "cancel", "12", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	item := envelope["data"].(map[string]any)["item"].(map[string]any)
	if envelope["operation"] != "queue.cancel" || item["item_id"] != float64(12) || item["status"] != "canceled" || item["batch_id"] != "batch-12" {
		t.Fatalf("envelope = %#v", envelope)
	}
	if cancels := server.cancels.Load(); cancels != 1 {
		t.Fatalf("sent %d cancel requests, want 1", cancels)
	}
}

func TestQueueCancelReportsInvokeAIStatusOfNamedItem(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name     string
		response func(http.ResponseWriter, int)
		want     string
	}{
		{name: "in-progress item", response: canceledItem("canceled", map[string]any{"started_at": "2026-01-01 00:00:01.000"}), want: "canceled"},
		{name: "already completed item", response: canceledItem("completed", map[string]any{"session": completedItemResults("output-12.png")}), want: "completed"},
		{name: "already failed item", response: canceledItem("failed", map[string]any{"error_type": "OutOfMemoryError", "error_message": "CUDA out of memory"}), want: "failed"},
		{name: "already canceled item", response: canceledItem("canceled", nil), want: "canceled"},
		// InvokeAI cancels the non-terminal items of the named item's
		// workflow-call chain and returns only the named item, which here was
		// already completed while its chain child was still pending.
		{name: "completed item in a workflow-call chain", response: canceledItem("completed", map[string]any{"parent_item_id": nil, "workflow_call_depth": 0}), want: "completed"},
		{name: "pending child in a workflow-call chain", response: canceledItem("canceled", map[string]any{"parent_item_id": 11, "workflow_call_depth": 1}), want: "canceled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newQueueCancelServer(t, "6.14.1", test.response)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "cancel", "12", "--url", server.URL)

			if exitCode != result.ExitSuccess || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			item := envelope["data"].(map[string]any)["item"].(map[string]any)
			if item["item_id"] != float64(12) || item["status"] != test.want {
				t.Fatalf("item = %#v, want item 12 with status %q", item, test.want)
			}
			if cancels := server.cancels.Load(); cancels != 1 {
				t.Fatalf("sent %d cancel requests, want 1", cancels)
			}
		})
	}
}

func TestQueueCancelProjectsTerminalItemLikeQueueGet(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueCancelServer(t, "6.14.1", canceledItem("failed", map[string]any{
		"error_type": "OutOfMemoryError", "error_message": "CUDA out of memory",
		"error_traceback": "Traceback: /server/private/path.py",
	}))

	exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "cancel", "12", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	item := envelope["data"].(map[string]any)["item"].(map[string]any)
	wantError := map[string]any{"type": "OutOfMemoryError", "message": "CUDA out of memory"}
	if !reflect.DeepEqual(item["error"], wantError) {
		t.Fatalf("item = %#v, want its concise error", item)
	}
	if encoded, _ := json.Marshal(envelope); strings.Contains(string(encoded), "Traceback") {
		t.Fatalf("server traceback leaked: %s", encoded)
	}
}

func TestQueueCancelAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueCancelServer(t, "6.14.1", canceledItem("canceled", nil))

	exitCode, envelope, stderr := runBoardsJSON(t, `{"schema_version":1,"queue_id":"default","item_id":7}`, "queue", "cancel", "--request", "-", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	item := envelope["data"].(map[string]any)["item"].(map[string]any)
	if item["item_id"] != float64(7) || item["status"] != "canceled" || server.cancels.Load() != 1 {
		t.Fatalf("envelope = %#v, cancels = %d", envelope, server.cancels.Load())
	}
}

func TestQueueCancelRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "no item id"},
		{name: "zero item id", args: []string{"0"}},
		{name: "negative item id", args: []string{"--", "-3"}},
		{name: "non-integer item id", args: []string{"seven"}},
		{name: "several item ids", args: []string{"4", "9"}},
		{name: "item id combined with request", stdin: `{"schema_version":1,"item_id":4}`, args: []string{"4", "--request", "-"}},
		{name: "queue id combined with request", stdin: `{"schema_version":1,"item_id":4}`, args: []string{"--queue-id", "default", "--request", "-"}},
		{name: "document without item id", stdin: `{"schema_version":1}`, args: []string{"--request", "-"}},
		{name: "document with zero item id", stdin: `{"schema_version":1,"item_id":0}`, args: []string{"--request", "-"}},
		{name: "document with empty queue id", stdin: `{"schema_version":1,"queue_id":"","item_id":4}`, args: []string{"--request", "-"}},
		{name: "document with batch selector", stdin: `{"schema_version":1,"batch_id":"batch-4"}`, args: []string{"--request", "-"}},
		{name: "document with item ids", stdin: `{"schema_version":1,"item_ids":[4,9]}`, args: []string{"--request", "-"}},
		{name: "document with unsupported schema version", stdin: `{"schema_version":2,"item_id":4}`, args: []string{"--request", "-"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newQueueCancelServer(t, "6.14.1", canceledItem("canceled", nil))
			args := append([]string{"queue", "cancel", "--url", server.URL}, test.args...)

			exitCode, envelope, stderr := runBoardsJSON(t, test.stdin, args...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "queue.cancel" || envelope["error"].(map[string]any)["code"] != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestQueueCancelSendsCancellationOnceOnEveryFailure(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name     string
		cancel   func(http.ResponseWriter, int)
		wantExit int
		wantCode string
	}{
		{
			name: "absent item",
			cancel: func(w http.ResponseWriter, itemID int) {
				http.Error(w, `{"detail":"Queue item with id `+strconv.Itoa(itemID)+` not found in queue default"}`, http.StatusNotFound)
			},
			wantExit: result.ExitInvokeAIFailure,
			wantCode: "not_found",
		},
		{
			name: "conclusive rejection",
			cancel: func(w http.ResponseWriter, _ int) {
				http.Error(w, `{"detail":"Unexpected error while canceling queue item"}`, http.StatusInternalServerError)
			},
			wantExit: result.ExitInvokeAIFailure,
			wantCode: "invokeai_operation_failed",
		},
		{
			name: "transient gateway status",
			cancel: func(w http.ResponseWriter, _ int) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			},
			wantExit: result.ExitInvokeAIFailure,
			wantCode: "invokeai_operation_failed",
		},
		{
			name: "lost response",
			cancel: func(w http.ResponseWriter, _ int) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack cancel connection: %v", err)
					return
				}
				_ = connection.Close()
			},
			wantExit: result.ExitInvokeAIFailure,
			wantCode: "outcome_unknown",
		},
		{
			name: "undecodable success",
			cancel: func(w http.ResponseWriter, _ int) {
				_, _ = w.Write([]byte(`{"item_id":`))
			},
			wantExit: result.ExitInvokeAIFailure,
			wantCode: "outcome_unknown",
		},
		{
			name: "success without the named item",
			cancel: func(w http.ResponseWriter, _ int) {
				_, _ = w.Write([]byte(`{}`))
			},
			wantExit: result.ExitInvokeAIFailure,
			wantCode: "outcome_unknown",
		},
		{
			name:     "success naming another item",
			cancel:   func(w http.ResponseWriter, _ int) { canceledItem("canceled", nil)(w, 13) },
			wantExit: result.ExitInvokeAIFailure,
			wantCode: "outcome_unknown",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newQueueCancelServer(t, "6.14.1", test.cancel)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "cancel", "12", "--url", server.URL)

			if exitCode != test.wantExit || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "queue.cancel" || envelope["error"].(map[string]any)["code"] != test.wantCode {
				t.Fatalf("envelope = %#v, want code %q", envelope, test.wantCode)
			}
			if cancels := server.cancels.Load(); cancels != 1 {
				t.Fatalf("sent %d cancel requests, want exactly 1", cancels)
			}
		})
	}
}

func TestQueueCancelRejectsUnsupportedInvokeAIVersionBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	for _, version := range []string{"6.14.0", "6.15.0"} {
		t.Run(version, func(t *testing.T) {
			server := newQueueCancelServer(t, version, canceledItem("canceled", nil))

			exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "cancel", "12", "--url", server.URL)

			if exitCode != result.ExitUnsupportedCapability || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["error"].(map[string]any)["code"] != "unsupported_capability" || server.cancels.Load() != 0 {
				t.Fatalf("envelope = %#v, cancels = %d", envelope, server.cancels.Load())
			}
		})
	}
}

func TestQueueCancelHumanOutputShowsNamedItem(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueCancelServer(t, "6.14.1", canceledItem("canceled", nil))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "cancel", "12", "--url", server.URL})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if want := "12\tcanceled\tbatch-12\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestQueueCancelReportsAppliedCancellationWhenItemProjectionFails(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name     string
		image    http.HandlerFunc
		wantExit int
		wantCode string
	}{
		{
			name:     "image read rejected",
			image:    func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
			wantExit: result.ExitInvokeAIFailure,
			wantCode: "invokeai_operation_failed",
		},
		{
			name: "image read connection lost",
			image: func(w http.ResponseWriter, _ *http.Request) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack image connection: %v", err)
					return
				}
				_ = connection.Close()
			},
			wantExit: result.ExitConnection,
			wantCode: "connection_failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newQueueCancelServerWithImages(t, "6.14.1",
				canceledItem("completed", map[string]any{"session": completedItemResults("output-12.png")}), test.image)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "queue", "cancel", "12", "--url", server.URL)

			if exitCode != test.wantExit || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			failure := envelope["error"].(map[string]any)
			details, _ := failure["details"].(map[string]any)
			if failure["code"] != test.wantCode || details["cancel_applied"] != true || details["queue_id"] != "default" ||
				details["item_id"] != float64(12) || details["item_status"] != "completed" {
				t.Fatalf("error = %#v, want code %q with the applied cancellation in its details", failure, test.wantCode)
			}
			if cancels := server.cancels.Load(); cancels != 1 {
				t.Fatalf("sent %d cancel requests, want exactly 1", cancels)
			}
		})
	}
}
