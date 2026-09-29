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
// later polls back off to MaxPollInterval.
const (
	FirstPollInterval = 100 * time.Millisecond
	MaxPollInterval   = time.Second
)

// InvokeAI 6.14 queue items use these statuses. pending, in_progress, and
// waiting are non-terminal; completed, failed, and canceled are terminal.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusWaiting    = "waiting"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCanceled   = "canceled"
)

// PollStep inspects one queue item. Each traversal chooses when to back off:
// between rounds for queue wait, or after a non-terminal graph item poll.
type PollStep struct {
	ctx, waitContext context.Context
	client           *httpclient.Client
	position         operation.QueuePosition
	mode             PollMode
	interval         time.Duration
}

// PollMode selects the identity and existing error contract for the caller.
// Traversal order remains the caller's responsibility.
type PollMode uint8

const (
	QueueWaitPoll PollMode = iota
	GraphOperationPoll
)

// PendingItems names the unobserved suffix of the current traversal. Earlier
// holds items already seen as non-terminal in this round of queue wait.
type PendingItems struct {
	Earlier         []int
	CurrentAndLater []int
}

func NewPollStep(ctx, waitContext context.Context, client *httpclient.Client, position operation.QueuePosition, mode PollMode) *PollStep {
	return &PollStep{ctx: ctx, waitContext: waitContext, client: client, position: position, mode: mode, interval: FirstPollInterval}
}

// Poll returns the inspected item and whether its tested status is terminal.
// pending names the items not yet observed as terminal if the wait stops.
func (step *PollStep) Poll(itemID int, pending PendingItems, backoff bool) (Item, bool, error) {
	if backoff {
		timer := time.NewTimer(step.interval)
		select {
		case <-step.waitContext.Done():
			timer.Stop()
			return Item{}, false, step.stopped(pending, 0, nil)
		case <-timer.C:
		}
		step.interval = min(step.interval*2, MaxPollInterval)
	}
	result, err := Get(step.waitContext, step.client, GetRequest{SchemaVersion: 1, QueueID: step.position.QueueID, ItemID: itemID})
	if err != nil {
		return Item{}, false, step.stopped(pending, itemID, err)
	}
	item := result.Item
	if item.ItemID != itemID || item.QueueID != step.position.QueueID || step.mode == GraphOperationPoll && item.BatchID != step.position.BatchID {
		detail := fmt.Sprintf("reported contradictory queue identity: item %d, queue %q", item.ItemID, item.QueueID)
		if step.mode == GraphOperationPoll {
			detail = fmt.Sprintf("reported contradictory queue identity: item %d, queue %q, batch %q", item.ItemID, item.QueueID, item.BatchID)
		}
		return Item{}, false, &operation.InvalidQueueResultError{Position: step.position, ItemID: itemID, Status: item.Status, Detail: detail}
	}
	switch item.Status {
	case StatusPending, StatusInProgress, StatusWaiting:
		return item, false, nil
	case StatusCompleted, StatusFailed, StatusCanceled:
		return item, true, nil
	default:
		return Item{}, false, &operation.InvalidQueueResultError{
			Position: step.position, ItemID: itemID, Status: item.Status,
			Detail: fmt.Sprintf("reported status %q, which is not a tested InvokeAI queue status", item.Status),
		}
	}
}

// stopped is the only wait stop reporter. Graph waits retain their existing
// message naming every accepted item while details name the unobserved suffix.
func (step *PollStep) stopped(pending PendingItems, itemID int, err error) error {
	var messageItemIDs []int
	if step.mode == GraphOperationPoll {
		messageItemIDs = step.position.ItemIDs
	}
	if step.ctx.Err() != nil {
		return &operation.InterruptedError{Position: step.position, PendingItemIDs: slices.Concat(pending.Earlier, pending.CurrentAndLater), MessageItemIDs: messageItemIDs}
	}
	if errors.Is(step.waitContext.Err(), context.DeadlineExceeded) {
		return &operation.WaitTimeoutError{Position: step.position, PendingItemIDs: slices.Concat(pending.Earlier, pending.CurrentAndLater), MessageItemIDs: messageItemIDs}
	}
	if httpError, ok := errors.AsType[*httpclient.HTTPError](err); step.mode == QueueWaitPoll && ok && httpError.StatusCode == http.StatusNotFound {
		return operation.NotFound(fmt.Sprintf("queue item %d was not found in queue %q", itemID, step.position.QueueID))
	}
	return err
}
