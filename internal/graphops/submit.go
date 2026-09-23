package graphops

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
)

type QueueReceipt struct {
	QueueID string `json:"queue_id"`
	BatchID string `json:"batch_id"`
	ItemIDs []int  `json:"item_ids"`
}

type Output struct {
	ItemID int              `json:"item_id"`
	Seed   uint32           `json:"seed"`
	Image  images.Reference `json:"image"`
}

type enqueueResponse struct {
	QueueID   string `json:"queue_id"`
	Enqueued  int    `json:"enqueued"`
	Requested int    `json:"requested"`
	Batch     struct {
		BatchID string `json:"batch_id"`
	} `json:"batch"`
	ItemIDs []int `json:"item_ids"`
}

// Enqueue sends exactly one mutation and validates the complete acceptance result.
func Enqueue(ctx context.Context, client *httpclient.Client, request EnqueueRequest, expectedItems int) (QueueReceipt, error) {
	var response enqueueResponse
	const enqueuePath = "/api/v1/queue/default/enqueue_batch"
	if err := client.DoJSON(ctx, http.MethodPost, enqueuePath, request, &response); err != nil {
		return QueueReceipt{}, err
	}
	if !validEnqueueResponse(response, expectedItems) {
		enqueueURL, err := client.ResolveURL(enqueuePath)
		if err != nil {
			return QueueReceipt{}, fmt.Errorf("resolve enqueue URL: %w", err)
		}
		return QueueReceipt{}, &httpclient.OutcomeUnknownError{
			Method: http.MethodPost,
			URL:    enqueueURL,
			Err:    errors.New("InvokeAI returned an incomplete enqueue result"),
		}
	}
	return QueueReceipt{QueueID: response.QueueID, BatchID: response.Batch.BatchID, ItemIDs: response.ItemIDs}, nil
}

func validEnqueueResponse(response enqueueResponse, expectedItems int) bool {
	if response.QueueID != "default" || response.Enqueued != expectedItems || response.Requested != expectedItems ||
		response.Batch.BatchID == "" || len(response.ItemIDs) != expectedItems {
		return false
	}
	seen := make(map[int]struct{}, len(response.ItemIDs))
	for _, itemID := range response.ItemIDs {
		if itemID < 1 {
			return false
		}
		if _, exists := seen[itemID]; exists {
			return false
		}
		seen[itemID] = struct{}{}
	}
	return true
}
