package queue

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type WaitRequest struct {
	SchemaVersion int    `json:"schema_version"`
	QueueID       string `json:"queue_id"`
	ItemIDs       []int  `json:"item_ids"`
}

// WaitOptions bounds local waiting. A zero Timeout waits without a total
// deadline.
type WaitOptions struct {
	Timeout time.Duration
}

// WaitResult holds each requested item's terminal queue-item projection in
// request order. A failed or canceled item is data, not an operation error.
type WaitResult struct {
	QueueID string `json:"queue_id"`
	Items   []Item `json:"items"`
}

// Wait observes the named queue items until each reaches a terminal status.
// Each round polls only the items that are still non-terminal, in request
// order. It never sends a mutation: a timeout or interruption stops only local
// waiting and reports the items that were not yet terminal.
func Wait(ctx context.Context, client *httpclient.Client, request WaitRequest, options WaitOptions) (WaitResult, error) {
	if err := validateWait(request); err != nil {
		return WaitResult{}, err
	}
	if options.Timeout < 0 {
		return WaitResult{}, operation.InvalidRequest("wait timeout cannot be negative")
	}
	waitContext := ctx
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		waitContext, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	position := operation.QueuePosition{QueueID: request.QueueID, ItemIDs: request.ItemIDs}
	step := NewPollStep(ctx, waitContext, client, position, QueueWaitPoll)
	items := make([]Item, len(request.ItemIDs))
	pending := slices.Clone(request.ItemIDs)
	backoff := false
	for {
		stillPending := make([]int, 0, len(pending))
		for index, itemID := range pending {
			item, terminal, err := step.Poll(itemID, PendingItems{Earlier: stillPending, CurrentAndLater: pending[index:]}, backoff && index == 0)
			if err != nil {
				return WaitResult{}, err
			}
			if !terminal {
				stillPending = append(stillPending, itemID)
			} else {
				items[slices.Index(request.ItemIDs, itemID)] = item
			}
		}
		pending = stillPending
		if len(pending) == 0 {
			return WaitResult{QueueID: request.QueueID, Items: items}, nil
		}
		backoff = true
	}
}

func validateWait(request WaitRequest) error {
	if request.SchemaVersion != 1 {
		return operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.QueueID == "" {
		return operation.InvalidRequest("queue id is required")
	}
	if len(request.ItemIDs) == 0 {
		return operation.InvalidRequest("at least one item id is required")
	}
	seen := make(map[int]struct{}, len(request.ItemIDs))
	for _, itemID := range request.ItemIDs {
		if itemID < 1 {
			return operation.InvalidRequest("item ids must be positive")
		}
		if _, ok := seen[itemID]; ok {
			return operation.InvalidRequest(fmt.Sprintf("item id %d is repeated", itemID))
		}
		seen[itemID] = struct{}{}
	}
	return nil
}
