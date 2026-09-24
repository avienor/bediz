package queue

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

// Waiting polls read-only queue inspection. The first poll is immediate and
// later polls back off to MaxPollInterval, so a quick item is observed within a
// tenth of a second while a long one costs at most one request per second.
const (
	FirstPollInterval = 100 * time.Millisecond
	MaxPollInterval   = time.Second
)

// InvokeAI 6.14 queue items use these statuses. pending, in_progress, and
// waiting are non-terminal; waiting means the item is suspended while a
// workflow-call child item runs. completed, failed, and canceled are terminal.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusWaiting    = "waiting"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCanceled   = "canceled"
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
	items := make([]Item, len(request.ItemIDs))
	pending := slices.Clone(request.ItemIDs)
	for interval := FirstPollInterval; ; interval = min(interval*2, MaxPollInterval) {
		stillPending := make([]int, 0, len(pending))
		for _, itemID := range pending {
			result, err := Get(waitContext, client, GetRequest{SchemaVersion: 1, QueueID: request.QueueID, ItemID: itemID})
			if err != nil {
				return WaitResult{}, waitStopped(ctx, waitContext, position, remaining(pending, itemID, stillPending), itemID, err)
			}
			item := result.Item
			if item.ItemID != itemID || item.QueueID != request.QueueID {
				return WaitResult{}, &operation.InvalidQueueResultError{
					Position: position, ItemID: itemID, Status: item.Status,
					Detail: fmt.Sprintf("reported contradictory queue identity: item %d, queue %q", item.ItemID, item.QueueID),
				}
			}
			switch item.Status {
			case StatusPending, StatusInProgress, StatusWaiting:
				stillPending = append(stillPending, itemID)
			case StatusCompleted, StatusFailed, StatusCanceled:
				items[slices.Index(request.ItemIDs, itemID)] = item
			default:
				return WaitResult{}, &operation.InvalidQueueResultError{
					Position: position, ItemID: itemID, Status: item.Status,
					Detail: fmt.Sprintf("reported status %q, which is not a tested InvokeAI queue status", item.Status),
				}
			}
		}
		pending = stillPending
		if len(pending) == 0 {
			return WaitResult{QueueID: request.QueueID, Items: items}, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-waitContext.Done():
			timer.Stop()
			return WaitResult{}, waitStopped(ctx, waitContext, position, pending, 0, nil)
		case <-timer.C:
		}
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

// remaining lists, in request order, the items not yet observed as terminal in
// the current round when polling stopped at itemID: the items already found
// non-terminal, itemID itself, and every later item of the round.
func remaining(round []int, itemID int, stillPending []int) []int {
	return slices.Concat(stillPending, round[slices.Index(round, itemID):])
}

// waitStopped reports why local waiting ended. A canceled caller context is a
// local interruption and an elapsed wait deadline is a timeout, each naming the
// pending items. An absent item is not found; any other failed read is returned
// unchanged.
func waitStopped(ctx, waitContext context.Context, position operation.QueuePosition, pending []int, itemID int, err error) error {
	if ctx.Err() != nil {
		return &operation.InterruptedError{Position: position, PendingItemIDs: pending}
	}
	if errors.Is(waitContext.Err(), context.DeadlineExceeded) {
		return &operation.WaitTimeoutError{Position: position, PendingItemIDs: pending}
	}
	if httpError, ok := errors.AsType[*httpclient.HTTPError](err); ok && httpError.StatusCode == http.StatusNotFound {
		return operation.NotFound(fmt.Sprintf("queue item %d was not found in queue %q", itemID, position.QueueID))
	}
	return err
}
