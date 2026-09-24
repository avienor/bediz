package queue

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

// ClearRequest names the queue to clear. Approved carries the --yes execution
// approval; it is not a Request Document field.
type ClearRequest struct {
	SchemaVersion int    `json:"schema_version"`
	QueueID       string `json:"queue_id"`
	Approved      bool   `json:"-"`
}

// ClearResult reports how many queue items InvokeAI deleted.
type ClearResult struct {
	QueueID string `json:"queue_id"`
	Deleted int    `json:"deleted"`
}

// Clear requires approval and a supported InvokeAI version, then sends one
// queue clear request and never retries it. InvokeAI 6.14.1 cancels and deletes
// every item in the queue for an admin caller, including the single-user
// default, and only the caller's own items for any other caller.
func Clear(ctx context.Context, client *httpclient.Client, request ClearRequest) (ClearResult, error) {
	if request.SchemaVersion != 1 {
		return ClearResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.QueueID == "" {
		return ClearResult{}, operation.InvalidRequest("queue id is required")
	}
	if !request.Approved {
		return ClearResult{}, operation.InvalidRequest("clearing a queue requires --yes")
	}

	if err := capability.RequireSupportedVersion(ctx, client); err != nil {
		return ClearResult{}, err
	}

	path := fmt.Sprintf("/api/v1/queue/%s/clear", url.PathEscape(request.QueueID))
	var response struct {
		Deleted *int `json:"deleted"`
	}
	if err := client.DoJSON(ctx, http.MethodPut, path, nil, &response); err != nil {
		return ClearResult{}, err
	}
	if response.Deleted == nil || *response.Deleted < 0 {
		return ClearResult{}, &httpclient.OutcomeUnknownError{
			Method: http.MethodPut, URL: path,
			Err: errors.New("clear response reports no deleted count"),
		}
	}
	return ClearResult{QueueID: request.QueueID, Deleted: *response.Deleted}, nil
}
