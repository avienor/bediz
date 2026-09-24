package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

// waitItemPayload is one InvokeAI 6.14.1 queue-item record for item itemID in
// the default queue.
func waitItemPayload(itemID int, status string, extra map[string]any) map[string]any {
	item := map[string]any{
		"item_id": itemID, "queue_id": "default", "batch_id": "batch-" + strconv.Itoa(itemID),
		"session_id": "session-" + strconv.Itoa(itemID), "status": status, "priority": 0,
		"created_at": "2026-01-01 00:00:00.000", "updated_at": "2026-01-01 00:00:00.000",
	}
	for key, value := range extra {
		item[key] = value
	}
	return item
}

type queueWaitServer struct {
	*httptest.Server
	mutex      sync.Mutex
	polls      map[int]int
	unexpected []string
}

// newQueueWaitServer serves read-only queue-item and image inspection. items
// holds one scripted payload per poll of each item and repeats its last entry;
// an item without a script is absent. Every other request is recorded as
// unexpected, so a test proves that waiting sends no cancel, enqueue, or other
// mutation. onPoll runs after each item response.
func newQueueWaitServer(t *testing.T, items map[int][]map[string]any, onPoll func(itemID, poll int)) *queueWaitServer {
	t.Helper()
	server := &queueWaitServer{polls: map[int]int{}}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			server.record(r)
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
			return
		}
		if value, ok := strings.CutPrefix(r.URL.Path, "/api/v1/queue/default/i/"); ok {
			itemID, err := strconv.Atoi(value)
			script, known := items[itemID]
			if err != nil || !known {
				http.Error(w, `{"detail":"Queue item not found"}`, http.StatusNotFound)
				return
			}
			server.mutex.Lock()
			poll := server.polls[itemID]
			server.polls[itemID]++
			server.mutex.Unlock()
			_ = json.NewEncoder(w).Encode(script[min(poll, len(script)-1)])
			if onPoll != nil {
				onPoll(itemID, poll)
			}
			return
		}
		if name, ok := strings.CutPrefix(r.URL.Path, "/api/v1/images/i/"); ok {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": name, "image_url": "api/v1/images/i/" + name + "/full",
				"thumbnail_url": "api/v1/images/i/" + name + "/thumbnail",
				"image_origin":  "internal", "image_category": "general", "width": 768, "height": 768,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false,
				"has_workflow": true,
			})
			return
		}
		server.record(r)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *queueWaitServer) record(r *http.Request) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.unexpected = append(s.unexpected, r.Method+" "+r.URL.Path)
}

func (s *queueWaitServer) assertReadOnly(t *testing.T) {
	t.Helper()
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(s.unexpected) != 0 {
		t.Fatalf("queue wait sent requests other than read-only inspection: %v", s.unexpected)
	}
}

func (s *queueWaitServer) pollCount(itemID int) int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.polls[itemID]
}

type queueWaitEnvelope struct {
	OK        bool   `json:"ok"`
	Operation string `json:"operation"`
	Data      struct {
		QueueID string           `json:"queue_id"`
		Items   []map[string]any `json:"items"`
	} `json:"data"`
	Error *struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func runQueueWait(t *testing.T, ctx context.Context, stdin string, args ...string) (int, queueWaitEnvelope, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(stdin), &stdout, &stderr)
	exitCode := app.Run(ctx, slices.Concat([]string{"queue", "wait", "--json"}, args))
	var envelope queueWaitEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q, stderr = %q", err, stdout.String(), stderr.String())
	}
	return exitCode, envelope, stdout.String(), stderr.String()
}

func itemIDsOf(items []map[string]any) []float64 {
	ids := make([]float64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item["item_id"].(float64))
	}
	return ids
}

func TestQueueWaitReturnsTerminalItemsInRequestOrder(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueWaitServer(t, map[int][]map[string]any{
		9: {
			waitItemPayload(9, "pending", nil),
			waitItemPayload(9, "in_progress", nil),
			waitItemPayload(9, "completed", map[string]any{"session": completedItemResults("output-9.png")}),
		},
		4: {waitItemPayload(4, "canceled", nil)},
		7: {
			waitItemPayload(7, "waiting", nil),
			waitItemPayload(7, "failed", map[string]any{
				"error_type": "OutOfMemoryError", "error_message": "CUDA out of memory",
				"error_traceback": "Traceback: /server/private/path.py",
			}),
		},
	}, nil)

	exitCode, envelope, stdout, stderr := runQueueWait(t, t.Context(), "", "9", "4", "7", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr, stdout)
	}
	if !envelope.OK || envelope.Operation != "queue.wait" || envelope.Data.QueueID != "default" {
		t.Fatalf("unexpected envelope: %s", stdout)
	}
	if ids := itemIDsOf(envelope.Data.Items); !slices.Equal(ids, []float64{9, 4, 7}) {
		t.Fatalf("item ids = %v, want request order [9 4 7]", ids)
	}
	completed, canceled, failed := envelope.Data.Items[0], envelope.Data.Items[1], envelope.Data.Items[2]
	images, _ := completed["images"].([]any)
	if completed["status"] != "completed" || len(images) != 1 || images[0].(map[string]any)["image_name"] != "output-9.png" {
		t.Fatalf("completed item = %#v, want its Image Reference", completed)
	}
	if canceled["status"] != "canceled" || canceled["batch_id"] != "batch-4" {
		t.Fatalf("canceled item = %#v", canceled)
	}
	wantError := map[string]any{"type": "OutOfMemoryError", "message": "CUDA out of memory"}
	if failed["status"] != "failed" || !reflect.DeepEqual(failed["error"], wantError) {
		t.Fatalf("failed item = %#v, want its concise error", failed)
	}
	if strings.Contains(stdout, "Traceback") || strings.Contains(stdout, "/server/private") {
		t.Fatalf("server traceback leaked: %s", stdout)
	}
	if server.pollCount(9) != 3 || server.pollCount(4) != 1 || server.pollCount(7) != 2 {
		t.Fatalf("polls = 9:%d 4:%d 7:%d, want each item polled until terminal and no further",
			server.pollCount(9), server.pollCount(4), server.pollCount(7))
	}
	server.assertReadOnly(t)
}

func TestQueueWaitAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueWaitServer(t, map[int][]map[string]any{
		4: {waitItemPayload(4, "completed", nil)},
		9: {waitItemPayload(9, "failed", nil)},
	}, nil)

	exitCode, envelope, stdout, stderr := runQueueWait(t, t.Context(),
		`{"schema_version":1,"queue_id":"default","item_ids":[4,9]}`,
		"--request", "-", "--timeout", "30s", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" || !envelope.OK {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr, stdout)
	}
	if ids := itemIDsOf(envelope.Data.Items); !slices.Equal(ids, []float64{4, 9}) {
		t.Fatalf("item ids = %v, want request order [4 9]", ids)
	}
	server.assertReadOnly(t)
}

func TestQueueWaitRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueWaitServer(t, map[int][]map[string]any{}, nil)
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "no item ids", args: nil},
		{name: "zero item id", args: []string{"0"}},
		{name: "negative item id", args: []string{"-3"}},
		{name: "negative item id after separator", args: []string{"--", "-3"}},
		{name: "non-integer item id", args: []string{"seven"}},
		{name: "duplicate item ids", args: []string{"4", "9", "4"}},
		{name: "negative timeout", args: []string{"4", "--timeout", "-1s"}},
		{name: "item ids combined with request", stdin: `{"schema_version":1,"item_ids":[4]}`, args: []string{"4", "--request", "-"}},
		{name: "queue id combined with request", stdin: `{"schema_version":1,"item_ids":[4]}`, args: []string{"--queue-id", "default", "--request", "-"}},
		{name: "document without item ids", stdin: `{"schema_version":1}`, args: []string{"--request", "-"}},
		{name: "document with empty item ids", stdin: `{"schema_version":1,"item_ids":[]}`, args: []string{"--request", "-"}},
		{name: "document with zero item id", stdin: `{"schema_version":1,"item_ids":[4,0]}`, args: []string{"--request", "-"}},
		{name: "document with duplicate item ids", stdin: `{"schema_version":1,"item_ids":[4,4]}`, args: []string{"--request", "-"}},
		{name: "document with empty queue id", stdin: `{"schema_version":1,"queue_id":"","item_ids":[4]}`, args: []string{"--request", "-"}},
		{name: "document with unknown field", stdin: `{"schema_version":1,"item_ids":[4],"cancel":true}`, args: []string{"--request", "-"}},
		{name: "document with unsupported schema version", stdin: `{"schema_version":2,"item_ids":[4]}`, args: []string{"--request", "-"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exitCode, envelope, stdout, stderr := runQueueWait(t, t.Context(), test.stdin, slices.Concat([]string{"--url", server.URL}, test.args)...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr, stdout)
			}
			if envelope.OK || envelope.Operation != "queue.wait" || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest {
				t.Fatalf("unexpected envelope: %s", stdout)
			}
		})
	}
	if len(server.polls) != 0 {
		t.Fatalf("invalid requests reached InvokeAI: %v", server.polls)
	}
	server.assertReadOnly(t)
}

func TestQueueWaitRejectsUntestedStatus(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueWaitServer(t, map[int][]map[string]any{
		4: {waitItemPayload(4, "completed", nil)},
		9: {waitItemPayload(9, "paused", nil)},
	}, nil)

	exitCode, envelope, stdout, _ := runQueueWait(t, t.Context(), "", "4", "9", "--url", server.URL)

	if exitCode != result.ExitInvokeAIFailure || envelope.OK || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidInvokeAIResponse {
		t.Fatalf("exit code = %d, stdout = %q", exitCode, stdout)
	}
	details := envelope.Error.Details
	if details["item_id"] != float64(9) || details["status"] != "paused" || details["queue_id"] != "default" {
		t.Fatalf("details = %#v, want the item and its untested status", details)
	}
	if _, ok := details["batch_id"]; ok {
		t.Fatalf("details = %#v, want no batch identity for queue wait", details)
	}
	if server.pollCount(9) != 1 {
		t.Fatalf("item 9 polls = %d, want the untested status to stop waiting", server.pollCount(9))
	}
	server.assertReadOnly(t)
}

func TestQueueWaitRejectsContradictoryItemIdentity(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueWaitServer(t, map[int][]map[string]any{
		4: {waitItemPayload(5, "completed", nil)},
	}, nil)

	exitCode, envelope, stdout, _ := runQueueWait(t, t.Context(), "", "4", "--url", server.URL)

	if exitCode != result.ExitInvokeAIFailure || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidInvokeAIResponse {
		t.Fatalf("exit code = %d, stdout = %q", exitCode, stdout)
	}
	server.assertReadOnly(t)
}

func TestQueueWaitReportsAbsentItemAsNotFound(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueWaitServer(t, map[int][]map[string]any{
		4: {waitItemPayload(4, "completed", nil)},
	}, nil)

	exitCode, envelope, stdout, _ := runQueueWait(t, t.Context(), "", "4", "12", "--url", server.URL)

	if exitCode != result.ExitInvokeAIFailure || envelope.Error == nil || envelope.Error.Code != result.CodeNotFound {
		t.Fatalf("exit code = %d, stdout = %q", exitCode, stdout)
	}
	if !strings.Contains(envelope.Error.Message, "12") {
		t.Fatalf("message = %q, want the absent item id", envelope.Error.Message)
	}
	server.assertReadOnly(t)
}

func TestQueueWaitTimeoutReportsPendingItemsWithoutCanceling(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueWaitServer(t, map[int][]map[string]any{
		9: {waitItemPayload(9, "in_progress", nil)},
		4: {waitItemPayload(4, "completed", nil)},
		7: {waitItemPayload(7, "pending", nil)},
	}, nil)

	exitCode, envelope, stdout, stderr := runQueueWait(t, t.Context(), "", "9", "4", "7", "--timeout", "250ms", "--url", server.URL)

	if exitCode != result.ExitInvokeAIFailure || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr, stdout)
	}
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != result.CodeWaitTimeout {
		t.Fatalf("unexpected envelope: %s", stdout)
	}
	details := envelope.Error.Details
	if details["queue_id"] != "default" ||
		!reflect.DeepEqual(details["item_ids"], []any{float64(9), float64(4), float64(7)}) ||
		!reflect.DeepEqual(details["pending_item_ids"], []any{float64(9), float64(7)}) {
		t.Fatalf("details = %#v, want requested and pending item ids in request order", details)
	}
	if server.pollCount(4) != 1 || server.pollCount(9) < 2 {
		t.Fatalf("polls = 9:%d 4:%d, want only non-terminal items polled again", server.pollCount(9), server.pollCount(4))
	}
	server.assertReadOnly(t)
}

func TestQueueWaitInterruptionReportsPendingItemsWithoutCanceling(t *testing.T) {
	isolateUserConfigDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := newQueueWaitServer(t, map[int][]map[string]any{
		4: {waitItemPayload(4, "completed", nil)},
		9: {waitItemPayload(9, "in_progress", nil)},
	}, func(itemID, _ int) {
		if itemID == 9 {
			cancel()
		}
	})

	exitCode, envelope, stdout, stderr := runQueueWait(t, ctx, "", "4", "9", "--url", server.URL)

	if exitCode != result.ExitInterrupted || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr, stdout)
	}
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != result.CodeInterrupted {
		t.Fatalf("unexpected envelope: %s", stdout)
	}
	if !reflect.DeepEqual(envelope.Error.Details["pending_item_ids"], []any{float64(9)}) {
		t.Fatalf("details = %#v, want the pending item id", envelope.Error.Details)
	}
	server.assertReadOnly(t)
}

func TestQueueWaitInterruptedMidRoundReportsEveryUnobservedItem(t *testing.T) {
	isolateUserConfigDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := newQueueWaitServer(t, map[int][]map[string]any{
		4: {waitItemPayload(4, "completed", nil)},
		9: {waitItemPayload(9, "in_progress", nil)},
		7: {waitItemPayload(7, "pending", nil)},
	}, func(itemID, _ int) {
		if itemID == 9 {
			cancel()
			// Hold the response so the interruption arrives while the read is in flight.
			time.Sleep(100 * time.Millisecond)
		}
	})

	exitCode, envelope, stdout, _ := runQueueWait(t, ctx, "", "4", "9", "7", "--url", server.URL)

	if exitCode != result.ExitInterrupted || envelope.Error == nil || envelope.Error.Code != result.CodeInterrupted {
		t.Fatalf("exit code = %d, stdout = %q", exitCode, stdout)
	}
	if !reflect.DeepEqual(envelope.Error.Details["pending_item_ids"], []any{float64(9), float64(7)}) {
		t.Fatalf("details = %#v, want the in-flight item and every later item of the round", envelope.Error.Details)
	}
	if server.pollCount(7) != 0 {
		t.Fatalf("item 7 polls = %d, want no poll after the interruption", server.pollCount(7))
	}
	server.assertReadOnly(t)
}

func TestQueueWaitHumanOutputListsItemsInRequestOrder(t *testing.T) {
	isolateUserConfigDir(t)
	server := newQueueWaitServer(t, map[int][]map[string]any{
		9: {waitItemPayload(9, "failed", map[string]any{"error_type": "OutOfMemoryError", "error_message": "CUDA out of memory"})},
		4: {waitItemPayload(4, "completed", nil)},
	}, nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "wait", "9", "4", "--url", server.URL})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if want := "9\tfailed\tbatch-9\tOutOfMemoryError: CUDA out of memory\n4\tcompleted\tbatch-4\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}
