package graphops_test

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
)

func TestWaitUsesTheGraphSeedField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/i/23":
			_ = json.MarshalWrite(w, map[string]any{
				"item_id": 23, "queue_id": "default", "batch_id": "batch-2", "session_id": "session-23",
				"status": "completed", "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01",
				"field_values": []map[string]any{{"node_path": "upscale_seed", "field_name": "value", "value": 42}},
				"session": map[string]any{"results": map[string]any{
					"decode": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "result.png"}},
				}},
			})
		case "/api/v1/images/i/result.png":
			_ = json.MarshalWrite(w, map[string]any{
				"image_name": "result.png", "image_url": "/api/v1/images/i/result.png/full",
				"thumbnail_url": "/api/v1/images/i/result.png/thumbnail", "image_origin": "internal", "image_category": "general",
				"width": 1024, "height": 1024, "created_at": "2026-01-01", "updated_at": "2026-01-01",
				"is_intermediate": false, "starred": false, "has_workflow": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := graphops.Wait(t.Context(), client,
		graphops.QueueReceipt{QueueID: "default", BatchID: "batch-2", ItemIDs: []int{23}},
		[]uint32{42}, graphops.SeedField{NodePath: "upscale_seed", FieldName: "value"}, graphops.WaitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 || outputs[0].ItemID != 23 || outputs[0].Seed != 42 || outputs[0].Image.ImageName != "result.png" {
		t.Fatalf("outputs = %#v", outputs)
	}
}
