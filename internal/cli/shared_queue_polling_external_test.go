package cli_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestGenerateWaitStopReportsUnobservedItemsInQueueOrder(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		firstDone  bool
	}{
		{"timeout at first item", result.CodeWaitTimeout, false},
		{"interrupted at first item", result.CodeInterrupted, false},
		{"timeout at second item", result.CodeWaitTimeout, true},
		{"interrupted at second item", result.CodeInterrupted, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var mu sync.Mutex
			var polls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/queue/default/enqueue_batch":
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 2, "requested": 2, "item_ids": []int{23, 22}, "batch": map[string]any{"batch_id": "batch-2"}})
				case "/api/v1/recall/default":
					_, _ = w.Write([]byte(`{"status":"success"}`))
				case "/api/v1/queue/default/i/23", "/api/v1/queue/default/i/22":
					mu.Lock()
					polls = append(polls, r.URL.Path)
					mu.Unlock()
					if r.URL.Path == "/api/v1/queue/default/i/23" && tc.firstDone {
						_ = json.MarshalWrite(w, completedBatchItem(23, "batch-2", 42, "first.png"))
					} else {
						itemID := 23
						if strings.HasSuffix(r.URL.Path, "/22") {
							itemID = 22
						}
						_ = json.MarshalWrite(w, map[string]any{"item_id": itemID, "queue_id": "default", "batch_id": "batch-2", "session_id": "session", "status": "pending", "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01"})
						if tc.code == result.CodeInterrupted {
							cancel()
						}
					}
				case "/api/v1/images/i/first.png":
					_ = json.MarshalWrite(w, upscaleImage("first.png", 1024, 1024))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			args := []string{"generate", "--model", "main-key", "--prompt", "test", "--seed", "42", "--output-count", "2", "--vae", "vae-key", "--qwen3-encoder", "encoder-key", "--url", server.URL, "--json"}
			if tc.code == result.CodeWaitTimeout {
				args = append(args, "--timeout", "500ms")
			}
			status := cli.New(&stdout, &stderr).Run(ctx, args)
			var envelope struct {
				Operation string        `json:"operation"`
				Error     *result.Error `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			wantPending := []int{23, 22}
			if tc.firstDone {
				wantPending = []int{22}
			}
			if status != result.ExitStatus(tc.code) || stderr.Len() != 0 || envelope.Operation != "generate" || envelope.Error == nil || envelope.Error.Code != tc.code || !reflect.DeepEqual(envelope.Error.Details["pending_item_ids"], jsonIDs(wantPending)) || !reflect.DeepEqual(envelope.Error.Details["item_ids"], jsonIDs([]int{23, 22})) || envelope.Error.Details["queue_id"] != "default" || envelope.Error.Details["batch_id"] != "batch-2" {
				t.Fatalf("status=%d stderr=%q error=%+v", status, stderr.String(), envelope.Error)
			}
			if tc.firstDone && !strings.Contains(envelope.Error.Message, "queue items 23, 22") {
				t.Fatalf("changed wait message: %q", envelope.Error.Message)
			}
			mu.Lock()
			defer mu.Unlock()
			if tc.firstDone && (len(polls) < 2 || !slices.Equal(polls[:2], []string{"/api/v1/queue/default/i/23", "/api/v1/queue/default/i/22"})) {
				t.Fatalf("poll order=%v", polls)
			}
			if !tc.firstDone && slices.Contains(polls, "/api/v1/queue/default/i/22") {
				t.Fatalf("second item polled before first became terminal: %v", polls)
			}
		})
	}
}

func TestUpscaleWaitStopReportsPendingItem(t *testing.T) {
	for _, tc := range []struct{ name, code string }{
		{"timeout", result.CodeWaitTimeout},
		{"interrupted", result.CodeInterrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/i/source.png":
					_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
				case "/api/v1/queue/default/enqueue_batch":
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
				case "/api/v1/queue/default/i/19":
					_ = json.MarshalWrite(w, map[string]any{"item_id": 19, "queue_id": "default", "batch_id": "upscale-batch", "session_id": "session-19", "status": "in_progress", "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01"})
					if tc.code == result.CodeInterrupted {
						cancel()
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			args := []string{"upscale", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42", "--url", server.URL, "--json"}
			if tc.code == result.CodeWaitTimeout {
				args = append(args, "--timeout", "150ms")
			}
			var stdout, stderr bytes.Buffer
			status := cli.New(&stdout, &stderr).Run(ctx, args)
			var envelope struct {
				Operation string        `json:"operation"`
				Error     *result.Error `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if status != result.ExitStatus(tc.code) || stderr.Len() != 0 || envelope.Operation != "upscale" || envelope.Error == nil || envelope.Error.Code != tc.code || !reflect.DeepEqual(envelope.Error.Details["pending_item_ids"], jsonIDs([]int{19})) || !reflect.DeepEqual(envelope.Error.Details["item_ids"], jsonIDs([]int{19})) || envelope.Error.Details["queue_id"] != "default" || envelope.Error.Details["batch_id"] != "upscale-batch" {
				t.Fatalf("status=%d stderr=%q error=%+v", status, stderr.String(), envelope.Error)
			}
		})
	}
}

func TestGenerateFailsAtFirstFailedItemBeforePollingLaterItems(t *testing.T) {
	isolateUserConfigDir(t)
	var mu sync.Mutex
	var polls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 2, "requested": 2, "item_ids": []int{23, 22}, "batch": map[string]any{"batch_id": "batch-2"}})
		case "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/queue/default/i/23", "/api/v1/queue/default/i/22":
			mu.Lock()
			polls = append(polls, r.URL.Path)
			mu.Unlock()
			item := completedBatchItem(23, "batch-2", 42, "unused.png")
			item["status"] = "failed"
			item["error_type"] = "ModelError"
			item["error_message"] = "generation failed"
			_ = json.MarshalWrite(w, item)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	status := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--model", "main-key", "--prompt", "test", "--seed", "42", "--output-count", "2", "--vae", "vae-key", "--qwen3-encoder", "encoder-key", "--url", server.URL, "--timeout", "150ms", "--json"})
	var envelope struct {
		Error *result.Error `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if status != result.ExitInvokeAIFailure || stderr.Len() != 0 || envelope.Error == nil || envelope.Error.Code != result.CodeInvokeAIOperationFailed || envelope.Error.Details["item_id"] != float64(23) || !slices.Equal(polls, []string{"/api/v1/queue/default/i/23"}) {
		t.Fatalf("status=%d stderr=%q error=%#v polls=%v", status, stderr.String(), envelope.Error, polls)
	}
}

func TestGenerateWaitPreservesMissingItemAndContradictoryBatchFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code, message string
		missing             bool
	}{
		{"missing item", result.CodeNotFound, "the requested InvokeAI resource was not found", true},
		{"contradictory batch", result.CodeInvalidInvokeAIResponse, `reported contradictory queue identity: item 17, queue "default", batch "other"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/queue/default/enqueue_batch":
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17}, "batch": map[string]any{"batch_id": "batch-1"}})
				case "/api/v1/recall/default":
					_, _ = w.Write([]byte(`{"status":"success"}`))
				case "/api/v1/queue/default/i/17":
					if tc.missing {
						http.NotFound(w, r)
					} else {
						_ = json.MarshalWrite(w, queueItemPayload("pending", map[string]any{"batch_id": "other"}))
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			status := cli.New(&stdout, &stderr).Run(t.Context(), generateWaitArgs(server.URL, "--json"))
			var envelope struct {
				Error *result.Error `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if status != result.ExitStatus(tc.code) || stderr.Len() != 0 || envelope.Error == nil || envelope.Error.Code != tc.code || !strings.Contains(envelope.Error.Message, tc.message) {
				t.Fatalf("status=%d stderr=%q error=%+v", status, stderr.String(), envelope.Error)
			}
		})
	}
}

func jsonIDs(ids []int) []any {
	values := make([]any, len(ids))
	for i, id := range ids {
		values[i] = float64(id)
	}
	return values
}
