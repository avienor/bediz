package generation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/queue"
)

// Waiting polls read-only queue inspection. The first poll is immediate and
// later polls back off to maxPollInterval, so a quick item is observed within a
// tenth of a second while a long one costs at most one request per second.
const (
	firstPollInterval = 100 * time.Millisecond
	maxPollInterval   = time.Second
)

// InvokeAI 6.14 queue items use these statuses. pending, in_progress, and
// waiting are non-terminal; waiting means the item is suspended while a
// workflow-call child item runs. completed, failed, and canceled are terminal.
const (
	statusPending    = "pending"
	statusInProgress = "in_progress"
	statusWaiting    = "waiting"
	statusCompleted  = "completed"
	statusFailed     = "failed"
	statusCanceled   = "canceled"
)

// WaitOptions bounds local waiting. A zero Timeout waits without a total
// deadline.
type WaitOptions struct {
	Timeout time.Duration
}

// Wait polls the accepted queue item until it reaches a terminal state and
// returns the completed Execution Receipt. Polling is read-only: the accepted
// enqueue is never replayed, and a local timeout or interruption stops only the
// wait, never the InvokeAI item.
func Wait(ctx context.Context, client *httpclient.Client, accepted ExecutionReceipt, options WaitOptions) (ExecutionReceipt, error) {
	if options.Timeout < 0 {
		return accepted, operation.InvalidRequest("wait timeout cannot be negative")
	}
	if len(accepted.Queue.ItemIDs) != 1 || len(accepted.ResolvedSettings.Seeds) != 1 {
		return accepted, operation.InvalidRequest(fmt.Sprintf(
			"wait requires exactly one accepted output: %d queue items and %d resolved seeds",
			len(accepted.Queue.ItemIDs), len(accepted.ResolvedSettings.Seeds),
		))
	}
	position := operation.QueuePosition{QueueID: accepted.Queue.QueueID, BatchID: accepted.Queue.BatchID, ItemIDs: accepted.Queue.ItemIDs}
	itemID := accepted.Queue.ItemIDs[0]

	waitContext := ctx
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		waitContext, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	for interval := firstPollInterval; ; interval = min(interval*2, maxPollInterval) {
		result, err := queue.Get(waitContext, client, queue.GetRequest{
			SchemaVersion: 1,
			QueueID:       accepted.Queue.QueueID,
			ItemID:        itemID,
		})
		if err != nil {
			return accepted, waitStopped(ctx, waitContext, position, err)
		}
		switch result.Item.Status {
		case statusPending, statusInProgress, statusWaiting:
		case statusCompleted:
			return completedReceipt(accepted, position, itemID, result.Item)
		case statusFailed, statusCanceled:
			failureType, failureMessage := normalizedFailure(result.Item)
			return accepted, &operation.ItemFailureError{
				Position: position, ItemID: itemID, Status: result.Item.Status,
				FailureType: failureType, FailureMessage: failureMessage,
			}
		default:
			return accepted, &operation.InvalidQueueResultError{
				Position: position, ItemID: itemID, Status: result.Item.Status,
				Detail: fmt.Sprintf("reported status %q, which is not a tested InvokeAI queue status", result.Item.Status),
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-waitContext.Done():
			timer.Stop()
			return accepted, waitStopped(ctx, waitContext, position, nil)
		case <-timer.C:
		}
	}
}

// completedReceipt associates the completed item with the resolved seed at the
// same position in the accepted Resolved Seed Set.
func completedReceipt(accepted ExecutionReceipt, position operation.QueuePosition, itemID int, item queue.Item) (ExecutionReceipt, error) {
	if len(item.Images) != 1 {
		return accepted, &operation.InvalidQueueResultError{
			Position: position, ItemID: itemID, Status: item.Status,
			Detail: fmt.Sprintf("completed with %d image outputs; expected exactly one", len(item.Images)),
		}
	}
	accepted.Outputs = []Output{{ItemID: itemID, Seed: accepted.ResolvedSettings.Seeds[0], Image: item.Images[0]}}
	return accepted, nil
}

// normalizedFailure returns the concise failure InvokeAI reports for a failed
// item. A server traceback is never part of the normalized queue item.
func normalizedFailure(item queue.Item) (failureType, failureMessage string) {
	if item.Error == nil {
		return "", ""
	}
	return item.Error.Type, item.Error.Message
}

// waitStopped reports why local waiting ended. A canceled caller context is a
// local interruption, an elapsed wait deadline is a timeout, and otherwise the
// failed read is returned unchanged.
func waitStopped(ctx, waitContext context.Context, position operation.QueuePosition, err error) error {
	if ctx.Err() != nil {
		return &operation.InterruptedError{Position: position}
	}
	if errors.Is(waitContext.Err(), context.DeadlineExceeded) {
		return &operation.WaitTimeoutError{Position: position}
	}
	return err
}
