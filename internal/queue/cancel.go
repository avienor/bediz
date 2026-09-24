package queue

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

// CancelRequest names one queue item to cancel. V1 has no batch or bulk
// selector.
type CancelRequest struct {
	SchemaVersion int    `json:"schema_version"`
	QueueID       string `json:"queue_id"`
	ItemID        int    `json:"item_id"`
}

// CancelResult is the queue get projection of the named item after the
// cancellation.
type CancelResult struct {
	Item Item `json:"item"`
}

// Cancel requires a supported InvokeAI version, then sends one per-item cancel
// request and never retries it. InvokeAI 6.14.1 cancels the non-terminal items
// of the named item's workflow-call chain, the named item included, and leaves
// terminal items unchanged; the result reflects its response for the named
// item.
func Cancel(ctx context.Context, client *httpclient.Client, request CancelRequest) (CancelResult, error) {
	if request.SchemaVersion != 1 {
		return CancelResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.QueueID == "" {
		return CancelResult{}, operation.InvalidRequest("queue id is required")
	}
	if request.ItemID < 1 {
		return CancelResult{}, operation.InvalidRequest("item id must be positive")
	}

	if err := capability.RequireSupportedVersion(ctx, client); err != nil {
		return CancelResult{}, err
	}

	path := fmt.Sprintf("/api/v1/queue/%s/i/%d/cancel", url.PathEscape(request.QueueID), request.ItemID)
	var response itemRecord
	if err := client.DoJSON(ctx, http.MethodPut, path, nil, &response); err != nil {
		return CancelResult{}, err
	}
	if response.ItemID != request.ItemID || response.QueueID != request.QueueID {
		return CancelResult{}, &httpclient.OutcomeUnknownError{
			Method: http.MethodPut, URL: path,
			Err: fmt.Errorf("cancel response names item %d in queue %q", response.ItemID, response.QueueID),
		}
	}
	item, err := projectItem(ctx, client, response)
	if err != nil {
		return CancelResult{}, &CancelAppliedError{QueueID: response.QueueID, ItemID: response.ItemID, Status: response.Status, Err: err}
	}
	return CancelResult{Item: item}, nil
}

// CancelAppliedError reports a failure after InvokeAI answered the cancellation
// with the named item: the cancellation was applied, and only building the item
// projection failed. Status is the item status InvokeAI returned.
type CancelAppliedError struct {
	QueueID string
	ItemID  int
	Status  string
	Err     error
}

func (e *CancelAppliedError) Error() string {
	return fmt.Sprintf("%v (the cancellation of queue item %d was applied; inspect it with queue get)", e.Err, e.ItemID)
}

func (e *CancelAppliedError) Unwrap() error { return e.Err }
