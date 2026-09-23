package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
)

type ListRequest struct {
	SchemaVersion int    `json:"schema_version"`
	QueueID       string `json:"queue_id"`
	Offset        int    `json:"offset"`
	Limit         int    `json:"limit"`
}

type Summary struct {
	ItemID       int     `json:"item_id"`
	QueueID      string  `json:"queue_id"`
	Status       string  `json:"status"`
	BatchID      string  `json:"batch_id"`
	Origin       *string `json:"origin,omitempty"`
	Destination  *string `json:"destination,omitempty"`
	CreatedAt    string  `json:"created_at"`
	StartedAt    *string `json:"started_at,omitempty"`
	CompletedAt  *string `json:"completed_at,omitempty"`
	Device       *string `json:"device,omitempty"`
	ParentItemID *int    `json:"parent_item_id,omitempty"`
}

type ListResult struct {
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
	Total  int       `json:"total"`
	Items  []Summary `json:"items"`
}

type GetRequest struct {
	SchemaVersion int    `json:"schema_version"`
	QueueID       string `json:"queue_id"`
	ItemID        int    `json:"item_id"`
}

type ItemError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

// BatchFieldValue is an InvokeAI batch substitution recorded on one queue
// item. It is retained for operation-specific receipt verification and is not
// part of Bediz's public queue-item JSON contract.
type BatchFieldValue struct {
	NodePath  string          `json:"node_path"`
	FieldName string          `json:"field_name"`
	Value     json.RawMessage `json:"value"`
}

type Item struct {
	ItemID      int                `json:"item_id"`
	QueueID     string             `json:"queue_id"`
	BatchID     string             `json:"batch_id"`
	SessionID   string             `json:"session_id"`
	Status      string             `json:"status"`
	Priority    int                `json:"priority"`
	Origin      *string            `json:"origin,omitempty"`
	Destination *string            `json:"destination,omitempty"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
	StartedAt   *string            `json:"started_at,omitempty"`
	CompletedAt *string            `json:"completed_at,omitempty"`
	Error       *ItemError         `json:"error,omitempty"`
	Images      []images.Reference `json:"images"`
	FieldValues []BatchFieldValue  `json:"-"`
	// ImageOutputCount preserves raw session-result multiplicity even when
	// duplicate names are normalized or an image can no longer be hydrated.
	ImageOutputCount int `json:"-"`
	// NonIntermediateImageOutputCount preserves multiplicity for image outputs
	// whose hydrated Image Reference is marked as a final gallery image.
	NonIntermediateImageOutputCount int    `json:"-"`
	ImageOutputValidationError      string `json:"-"`
}

type GetResult struct {
	Item Item `json:"item"`
}

type itemIDsResponse struct {
	ItemIDs    []int `json:"item_ids"`
	TotalCount int   `json:"total_count"`
}

type summariesRequest struct {
	ItemIDs []int `json:"item_ids"`
}

type summaryRecord struct {
	ItemID       int     `json:"item_id"`
	Status       string  `json:"status"`
	BatchID      string  `json:"batch_id"`
	Origin       *string `json:"origin"`
	Destination  *string `json:"destination"`
	CreatedAt    string  `json:"created_at"`
	StartedAt    *string `json:"started_at"`
	CompletedAt  *string `json:"completed_at"`
	Device       *string `json:"device"`
	ParentItemID *int    `json:"parent_item_id"`
}

type itemRecord struct {
	ItemID       int               `json:"item_id"`
	QueueID      string            `json:"queue_id"`
	BatchID      string            `json:"batch_id"`
	SessionID    string            `json:"session_id"`
	Status       string            `json:"status"`
	Priority     int               `json:"priority"`
	Origin       *string           `json:"origin"`
	Destination  *string           `json:"destination"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
	StartedAt    *string           `json:"started_at"`
	CompletedAt  *string           `json:"completed_at"`
	ErrorType    *string           `json:"error_type"`
	ErrorMessage *string           `json:"error_message"`
	FieldValues  []BatchFieldValue `json:"field_values"`
	Session      struct {
		Results map[string]json.RawMessage `json:"results"`
	} `json:"session"`
}

type outputRecord struct {
	Type  string `json:"type"`
	Image struct {
		ImageName string `json:"image_name"`
	} `json:"image"`
}

type outputTypeRecord struct {
	Type string `json:"type"`
}

func List(ctx context.Context, client *httpclient.Client, request ListRequest) (ListResult, error) {
	if request.SchemaVersion != 1 {
		return ListResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.QueueID == "" {
		return ListResult{}, operation.InvalidRequest("queue id is required")
	}
	if request.Offset < 0 {
		return ListResult{}, operation.InvalidRequest("offset must be non-negative")
	}
	if request.Limit < 1 || request.Limit > 100 {
		return ListResult{}, operation.InvalidRequest("limit must be between 1 and 100")
	}

	basePath := "/api/v1/queue/" + url.PathEscape(request.QueueID)
	query := url.Values{"order_dir": []string{"DESC"}}
	var ids itemIDsResponse
	if err := client.GetJSON(ctx, basePath+"/item_ids?"+query.Encode(), &ids); err != nil {
		return ListResult{}, err
	}
	result := ListResult{Offset: request.Offset, Limit: request.Limit, Total: ids.TotalCount, Items: []Summary{}}
	if request.Offset >= len(ids.ItemIDs) {
		return result, nil
	}
	end := min(request.Offset+request.Limit, len(ids.ItemIDs))
	page := ids.ItemIDs[request.Offset:end]
	var records []summaryRecord
	if err := client.QueryJSON(ctx, basePath+"/item_summaries_by_ids", summariesRequest{ItemIDs: page}, &records); err != nil {
		return ListResult{}, err
	}
	recordsByID := make(map[int]summaryRecord, len(records))
	for _, item := range records {
		recordsByID[item.ItemID] = item
	}
	result.Items = make([]Summary, 0, len(records))
	for _, itemID := range page {
		item, ok := recordsByID[itemID]
		if !ok {
			continue
		}
		result.Items = append(result.Items, Summary{
			ItemID:       item.ItemID,
			QueueID:      request.QueueID,
			Status:       item.Status,
			BatchID:      item.BatchID,
			Origin:       item.Origin,
			Destination:  item.Destination,
			CreatedAt:    item.CreatedAt,
			StartedAt:    item.StartedAt,
			CompletedAt:  item.CompletedAt,
			Device:       item.Device,
			ParentItemID: item.ParentItemID,
		})
	}
	return result, nil
}

func Get(ctx context.Context, client *httpclient.Client, request GetRequest) (GetResult, error) {
	if request.SchemaVersion != 1 {
		return GetResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.QueueID == "" {
		return GetResult{}, operation.InvalidRequest("queue id is required")
	}
	if request.ItemID < 1 {
		return GetResult{}, operation.InvalidRequest("item id must be positive")
	}

	path := fmt.Sprintf("/api/v1/queue/%s/i/%d", url.PathEscape(request.QueueID), request.ItemID)
	var response itemRecord
	if err := client.GetJSON(ctx, path, &response); err != nil {
		return GetResult{}, err
	}
	item := Item{
		ItemID:      response.ItemID,
		QueueID:     response.QueueID,
		BatchID:     response.BatchID,
		SessionID:   response.SessionID,
		Status:      response.Status,
		Priority:    response.Priority,
		Origin:      response.Origin,
		Destination: response.Destination,
		CreatedAt:   response.CreatedAt,
		UpdatedAt:   response.UpdatedAt,
		StartedAt:   response.StartedAt,
		CompletedAt: response.CompletedAt,
		Images:      []images.Reference{},
		FieldValues: response.FieldValues,
	}
	if response.ErrorType != nil || response.ErrorMessage != nil {
		item.Error = &ItemError{}
		if response.ErrorType != nil {
			item.Error.Type = *response.ErrorType
		}
		if response.ErrorMessage != nil {
			item.Error.Message = *response.ErrorMessage
		}
	}

	imageNames := make(map[string]struct{}, len(response.Session.Results))
	outputNames := make([]string, 0, len(response.Session.Results))
	for _, raw := range response.Session.Results {
		var outputType outputTypeRecord
		if err := json.Unmarshal(raw, &outputType); err != nil || outputType.Type != "image_output" {
			continue
		}
		item.ImageOutputCount++
		var output outputRecord
		if err := json.Unmarshal(raw, &output); err != nil {
			if item.ImageOutputValidationError == "" {
				item.ImageOutputValidationError = "completed session contains a malformed image output"
			}
			continue
		}
		if output.Image.ImageName == "" {
			if item.ImageOutputValidationError == "" {
				item.ImageOutputValidationError = "completed session contains an image output without an image name"
			}
			continue
		}
		imageNames[output.Image.ImageName] = struct{}{}
		outputNames = append(outputNames, output.Image.ImageName)
	}
	hydrated := make(map[string]images.Reference, len(imageNames))
	for _, imageName := range slices.Sorted(maps.Keys(imageNames)) {
		image, err := images.Get(ctx, client, images.GetRequest{SchemaVersion: 1, ImageName: imageName})
		if err != nil {
			if httpError, ok := errors.AsType[*httpclient.HTTPError](err); ok && httpError.StatusCode == http.StatusNotFound {
				continue
			}
			return GetResult{}, fmt.Errorf("get output image %q: %w", imageName, err)
		}
		if image.Image.ImageName != imageName {
			if item.ImageOutputValidationError == "" {
				item.ImageOutputValidationError = fmt.Sprintf(
					"hydrated image output has contradictory image name %q; expected %q", image.Image.ImageName, imageName,
				)
			}
			continue
		}
		item.Images = append(item.Images, image.Image)
		hydrated[imageName] = image.Image
	}
	for _, imageName := range outputNames {
		if image, ok := hydrated[imageName]; ok && !image.IsIntermediate {
			item.NonIntermediateImageOutputCount++
		}
	}
	return GetResult{Item: item}, nil
}
