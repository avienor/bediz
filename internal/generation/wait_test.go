package generation_test

import (
	json "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

func TestWaitRejectsMissingOrContradictorySeedMetadata(t *testing.T) {
	for _, test := range []struct {
		name        string
		fieldValues any
		wantDetail  string
	}{
		{name: "missing", fieldValues: nil, wantDetail: "missing the resolved seed"},
		{
			name: "contradictory",
			fieldValues: []map[string]any{
				{"node_path": "seed", "field_name": "value", "value": 99},
			},
			wantDetail: "seed 99 contradicts resolved seed 42",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/queue/default/i/23":
					_ = json.MarshalWrite(w, map[string]any{
						"item_id": 23, "queue_id": "default", "batch_id": "batch-2", "session_id": "session-23",
						"status": "completed", "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01",
						"field_values": test.fieldValues,
						"session": map[string]any{"results": map[string]any{
							"decode": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "first.png"}},
						}},
					})
				case "/api/v1/images/i/first.png":
					_ = json.MarshalWrite(w, testImagePayload("first.png"))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client, err := httpclient.New(server.URL, "", httpclient.Options{})
			if err != nil {
				t.Fatal(err)
			}

			_, err = generation.Wait(t.Context(), client, acceptedBatchReceipt([]int{23}, []uint32{42}), generation.WaitOptions{})
			invalid, ok := errors.AsType[*operation.InvalidQueueResultError](err)
			if !ok || invalid.ItemID != 23 || !strings.Contains(invalid.Detail, test.wantDetail) {
				t.Fatalf("error = %#v, want invalid queue result containing %q", err, test.wantDetail)
			}
		})
	}
}

func TestWaitReportsACompletedThenFailedBatchAsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/i/23":
			_ = json.MarshalWrite(w, map[string]any{
				"item_id": 23, "queue_id": "default", "batch_id": "batch-2", "session_id": "session-23",
				"status": "completed", "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01",
				"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 42}},
				"session": map[string]any{"results": map[string]any{
					"decode": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "first.png"}},
				}},
			})
		case "/api/v1/queue/default/i/22":
			_ = json.MarshalWrite(w, map[string]any{
				"item_id": 22, "queue_id": "default", "batch_id": "batch-2", "session_id": "session-22",
				"status": "failed", "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01",
				"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 43}},
				"error_type":   "RuntimeError", "error_message": "second output failed",
			})
		case "/api/v1/images/i/first.png":
			_ = json.MarshalWrite(w, testImagePayload("first.png"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{})
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := generation.Wait(
		t.Context(), client, acceptedBatchReceipt([]int{23, 22}, []uint32{42, 43}), generation.WaitOptions{},
	)
	failure, ok := errors.AsType[*operation.ItemFailureError](err)
	if !ok || failure.ItemID != 22 || failure.Status != "failed" || len(receipt.Outputs) != 1 {
		t.Fatalf("receipt = %#v, error = %#v", receipt, err)
	}
}

func acceptedBatchReceipt(itemIDs []int, seeds []uint32) generation.ExecutionReceipt {
	return generation.ExecutionReceipt{
		ResolvedSettings: generation.ResolvedSettings{OutputCount: len(seeds), Seeds: seeds},
		Queue:            generation.QueueReceipt{QueueID: "default", BatchID: "batch-2", ItemIDs: itemIDs},
		Outputs:          []generation.Output{},
	}
}

func testImagePayload(name string) map[string]any {
	return map[string]any{
		"image_name": name, "image_url": "/api/v1/images/i/" + name + "/full",
		"thumbnail_url": "/api/v1/images/i/" + name + "/thumbnail",
		"image_origin":  "internal", "image_category": "general", "width": 1024, "height": 1024,
		"created_at": "2026-01-01", "updated_at": "2026-01-01", "is_intermediate": false,
		"starred": false, "has_workflow": false,
	}
}
