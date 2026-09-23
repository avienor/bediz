package generation

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/result"
)

type ResolvedSettings struct {
	PositivePrompt string            `json:"positive_prompt"`
	NegativePrompt string            `json:"negative_prompt"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	Steps          int               `json:"steps"`
	Scheduler      string            `json:"scheduler"`
	Guidance       *float64          `json:"guidance,omitempty"`
	OutputCount    int               `json:"output_count"`
	BoardID        string            `json:"board_id,omitempty"`
	ModelKey       string            `json:"model_key"`
	ComponentKeys  map[string]string `json:"component_keys"`
	Seeds          []uint32          `json:"seeds"`
}

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

type ExecutionReceipt struct {
	Family           string           `json:"-"`
	SubmittedRequest Request          `json:"submitted_request"`
	ResolvedSettings ResolvedSettings `json:"resolved_settings"`
	Queue            QueueReceipt     `json:"queue"`
	Outputs          []Output         `json:"outputs"`
	Warnings         []result.Warning `json:"warnings"`
}

type modelListResponse struct {
	Models []ModelIdentifier `json:"models"`
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

func Submit(ctx context.Context, client *httpclient.Client, request Request) (ExecutionReceipt, error) {
	if err := validateCommonRequest(request); err != nil {
		return ExecutionReceipt{}, err
	}
	var versionResponse struct {
		Version string `json:"version"`
	}
	if err := client.GetJSON(ctx, "/api/v1/app/version", &versionResponse); err != nil {
		return ExecutionReceipt{}, err
	}
	supported, err := capability.SupportsInvokeAI(versionResponse.Version)
	if err != nil {
		return ExecutionReceipt{}, fmt.Errorf("validate InvokeAI version: %w", err)
	}
	if !supported {
		return ExecutionReceipt{}, operation.UnsupportedCapability(fmt.Sprintf(
			"InvokeAI %s is outside the supported range %s", versionResponse.Version, capability.SupportedInvokeAIRange,
		))
	}
	var inventory modelListResponse
	if err := client.GetJSON(ctx, "/api/v2/models/", &inventory); err != nil {
		return ExecutionReceipt{}, err
	}
	main, err := ResolveFamilyMain(inventory.Models, request.Model)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	adapter, err := adapterForBase(main.Base)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	var openAPI openAPIDocument
	if err := client.GetJSON(ctx, "/openapi.json", &openAPI); err != nil {
		return ExecutionReceipt{}, err
	}
	if err := validateFamilyOpenAPI(openAPI, adapter.invocations()); err != nil {
		return ExecutionReceipt{}, err
	}
	resolved, err := adapter.resolve(request, main, inventory.Models, rand.Reader)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	enqueueRequest, err := adapter.compile(resolved)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	var response enqueueResponse
	const enqueuePath = "/api/v1/queue/default/enqueue_batch"
	if err := client.DoJSON(ctx, http.MethodPost, enqueuePath, enqueueRequest, &response); err != nil {
		return ExecutionReceipt{}, err
	}
	if !validEnqueueResponse(response, *resolved.Request.OutputCount) {
		enqueueURL, err := client.ResolveURL(enqueuePath)
		if err != nil {
			return ExecutionReceipt{}, fmt.Errorf("resolve enqueue URL: %w", err)
		}
		return ExecutionReceipt{}, &httpclient.OutcomeUnknownError{
			Method: http.MethodPost,
			URL:    enqueueURL,
			Err:    errors.New("InvokeAI returned an incomplete enqueue result"),
		}
	}

	return ExecutionReceipt{
		Family:           main.Base,
		SubmittedRequest: request,
		ResolvedSettings: ResolvedSettings{
			PositivePrompt: resolved.Request.PositivePrompt,
			NegativePrompt: resolved.Request.NegativePrompt,
			Width:          *resolved.Request.Width,
			Height:         *resolved.Request.Height,
			Steps:          *resolved.Request.Steps,
			Scheduler:      *resolved.Request.Scheduler,
			Guidance:       resolved.Request.Guidance,
			OutputCount:    *resolved.Request.OutputCount,
			BoardID:        resolved.Request.BoardID,
			ModelKey:       resolved.Models.Main.Key,
			ComponentKeys:  adapter.componentKeys(resolved),
			Seeds:          slices.Clone(resolved.Seeds),
		},
		Queue:    QueueReceipt{QueueID: response.QueueID, BatchID: response.Batch.BatchID, ItemIDs: response.ItemIDs},
		Outputs:  []Output{},
		Warnings: []result.Warning{},
	}, nil
}

func validEnqueueResponse(response enqueueResponse, outputCount int) bool {
	if response.QueueID != "default" || response.Enqueued != outputCount || response.Requested != outputCount ||
		response.Batch.BatchID == "" || len(response.ItemIDs) != outputCount {
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
