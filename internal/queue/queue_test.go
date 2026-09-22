package queue_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/queue"
)

func TestListBoundsSummaryHydrationForLargeItemIndex(t *testing.T) {
	const (
		itemCount = 10_000
		offset    = 5_432
		limit     = 7
	)
	itemIDs := make([]int, itemCount)
	for i := range itemCount {
		itemIDs[i] = itemCount - i
	}
	wantPage := slices.Clone(itemIDs[offset : offset+limit])

	var summaryRequests atomic.Int32
	var graphRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/item_ids":
			if r.Method != http.MethodGet || r.URL.Query().Get("order_dir") != "DESC" {
				t.Errorf("unexpected item id request: %s %s", r.Method, r.URL.String())
			}
			if err := json.MarshalWrite(w, map[string]any{"item_ids": itemIDs, "total_count": itemCount}); err != nil {
				t.Errorf("encode item id response: %v", err)
			}
		case "/api/v1/queue/default/item_summaries_by_ids":
			summaryRequests.Add(1)
			var request struct {
				ItemIDs []int `json:"item_ids"`
			}
			if err := json.UnmarshalRead(r.Body, &request); err != nil {
				t.Errorf("decode summary request: %v", err)
				return
			}
			if !slices.Equal(request.ItemIDs, wantPage) {
				t.Errorf("hydrated item ids = %v, want %v", request.ItemIDs, wantPage)
			}
			records := make([]map[string]any, 0, len(request.ItemIDs))
			for i := len(request.ItemIDs) - 1; i >= 0; i-- {
				itemID := request.ItemIDs[i]
				records = append(records, map[string]any{
					"item_id": itemID, "status": "completed", "batch_id": fmt.Sprintf("batch-%d", itemID),
					"created_at": fmt.Sprintf("created-%d", itemID),
				})
			}
			if err := json.MarshalWrite(w, records); err != nil {
				t.Errorf("encode summary response: %v", err)
			}
		default:
			if strings.Contains(r.URL.Path, "/i/") {
				graphRequests.Add(1)
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := queue.List(t.Context(), client, queue.ListRequest{
		SchemaVersion: 1,
		QueueID:       "default",
		Offset:        offset,
		Limit:         limit,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Total != itemCount || result.Offset != offset || result.Limit != limit || len(result.Items) != limit {
		t.Fatalf("unexpected bounded result: %#v", result)
	}
	for i, item := range result.Items {
		if item.ItemID != wantPage[i] {
			t.Fatalf("item %d id = %d, want %d; items = %#v", i, item.ItemID, wantPage[i], result.Items)
		}
	}
	if summaryRequests.Load() != 1 || graphRequests.Load() != 0 {
		t.Fatalf("summary requests = %d, graph requests = %d, want 1 and 0", summaryRequests.Load(), graphRequests.Load())
	}
}

func TestListRejectsItemIndexAboveHTTPResponseSizeLimit(t *testing.T) {
	const maxBody = int64(128)
	var summaryRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/item_ids":
			itemIDs := make([]int, 100)
			for i := range itemIDs {
				itemIDs[i] = i + 1
			}
			if err := json.MarshalWrite(w, map[string]any{"item_ids": itemIDs, "total_count": len(itemIDs)}); err != nil {
				t.Errorf("encode item id response: %v", err)
			}
		case "/api/v1/queue/default/item_summaries_by_ids":
			summaryRequests.Add(1)
			http.Error(w, "summary hydration must not follow an oversized index", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client(), MaxBody: maxBody})
	if err != nil {
		t.Fatal(err)
	}
	_, err = queue.List(t.Context(), client, queue.ListRequest{SchemaVersion: 1, QueueID: "default", Limit: 1})

	invalidResponse, ok := errors.AsType[*httpclient.InvalidResponseError](err)
	if !ok || !strings.Contains(invalidResponse.Error(), "response body exceeds 128 bytes") {
		t.Fatalf("error = %#v, want HTTP response-size failure", err)
	}
	if summaryRequests.Load() != 0 {
		t.Fatalf("summary requests = %d, want 0", summaryRequests.Load())
	}
}
