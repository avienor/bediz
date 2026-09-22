package generation

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
)

type ResolvedSettings struct {
	PositivePrompt string            `json:"positive_prompt"`
	NegativePrompt string            `json:"negative_prompt"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	Steps          int               `json:"steps"`
	Scheduler      string            `json:"scheduler"`
	Guidance       float64           `json:"guidance"`
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
	SubmittedRequest Request          `json:"submitted_request"`
	ResolvedSettings ResolvedSettings `json:"resolved_settings"`
	Queue            QueueReceipt     `json:"queue"`
	Outputs          []Output         `json:"outputs"`
}

type modelRecord struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
	Name string `json:"name"`
	Base string `json:"base"`
	Type string `json:"type"`
}

type modelListResponse struct {
	Models []modelRecord `json:"models"`
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
	if err := validateExactRequest(request); err != nil {
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
	mainModel, err := resolveModel(inventory.Models, request.Model, "main_model", "anima", "main")
	if err != nil {
		return ExecutionReceipt{}, fmt.Errorf("resolve Anima main model: %w", err)
	}
	vae, err := resolveModel(inventory.Models, request.Components.VAE, "vae", "anima", "vae")
	if err != nil {
		return ExecutionReceipt{}, fmt.Errorf("resolve Anima VAE: %w", err)
	}
	qwen3Encoder, err := resolveModel(inventory.Models, request.Components.Qwen3Encoder, "qwen3_encoder", "any", "qwen3_encoder")
	if err != nil {
		return ExecutionReceipt{}, fmt.Errorf("resolve Qwen3 encoder: %w", err)
	}
	models := ResolvedModels{Main: mainModel, VAE: vae, Qwen3Encoder: qwen3Encoder}
	enqueueRequest, err := CompileAnima(request, models)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	var response enqueueResponse
	const enqueuePath = "/api/v1/queue/default/enqueue_batch"
	if err := client.DoJSON(ctx, http.MethodPost, enqueuePath, enqueueRequest, &response); err != nil {
		return ExecutionReceipt{}, err
	}
	if response.QueueID != "default" || response.Enqueued != 1 || response.Requested != 1 ||
		response.Batch.BatchID == "" || len(response.ItemIDs) != 1 || response.ItemIDs[0] < 1 {
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
		SubmittedRequest: request,
		ResolvedSettings: ResolvedSettings{
			PositivePrompt: request.PositivePrompt,
			NegativePrompt: request.NegativePrompt,
			Width:          *request.Width,
			Height:         *request.Height,
			Steps:          *request.Steps,
			Scheduler:      *request.Scheduler,
			Guidance:       *request.Guidance,
			OutputCount:    *request.OutputCount,
			BoardID:        request.BoardID,
			ModelKey:       mainModel.Key,
			ComponentKeys:  map[string]string{"vae": vae.Key, "qwen3_encoder": qwen3Encoder.Key},
			Seeds:          []uint32{*request.Seed},
		},
		Queue:   QueueReceipt{QueueID: response.QueueID, BatchID: response.Batch.BatchID, ItemIDs: response.ItemIDs},
		Outputs: []Output{},
	}, nil
}

func validateExactRequest(request Request) error {
	if request.SchemaVersion != 1 {
		return operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.Model == "" {
		return operation.InvalidRequest("model is required")
	}
	if request.PositivePrompt == "" {
		return operation.InvalidRequest("positive prompt is required")
	}
	if request.Width == nil || request.Height == nil || request.Steps == nil || request.Scheduler == nil ||
		request.Guidance == nil || request.Seed == nil || request.OutputCount == nil {
		return operation.InvalidRequest("width, height, steps, scheduler, guidance, seed, and output count are required for exact Anima generation")
	}
	if *request.Width < 1 || *request.Width%8 != 0 || *request.Height < 1 || *request.Height%8 != 0 {
		return operation.InvalidRequest("width and height must be positive multiples of 8")
	}
	if *request.Steps < 1 {
		return operation.InvalidRequest("steps must be positive")
	}
	if !slices.Contains([]string{"euler", "heun", "dpmpp_2m", "dpmpp_2m_sde", "er_sde", "lcm"}, *request.Scheduler) {
		return operation.InvalidRequest("scheduler is not supported for Anima")
	}
	if math.IsNaN(*request.Guidance) || math.IsInf(*request.Guidance, 0) || *request.Guidance < 1 {
		return operation.InvalidRequest("guidance must be finite and at least 1")
	}
	if *request.OutputCount != 1 {
		return operation.InvalidRequest("output count must be 1 for this Anima generation capability")
	}
	if request.Components == nil || request.Components.VAE == "" || request.Components.Qwen3Encoder == "" {
		return operation.InvalidRequest("explicit VAE and Qwen3 encoder selectors are required for exact Anima generation")
	}
	return nil
}

func resolveModel(models []modelRecord, selector, kind, expectedBase, expectedType string) (ModelIdentifier, error) {
	for _, model := range models {
		if model.Key != selector {
			continue
		}
		if model.Base != expectedBase || model.Type != expectedType {
			return ModelIdentifier{}, operation.UnsupportedCapability(fmt.Sprintf(
				"model %q has base %q and type %q; expected base %q and type %q",
				selector, model.Base, model.Type, expectedBase, expectedType,
			))
		}
		return model.identifier()
	}
	var matches []modelRecord
	for _, model := range models {
		if model.Name == selector && model.Base == expectedBase && model.Type == expectedType {
			matches = append(matches, model)
		}
	}
	if len(matches) == 1 {
		return matches[0].identifier()
	}
	if len(matches) > 1 {
		slices.SortFunc(matches, func(a, b modelRecord) int { return cmp.Compare(a.Key, b.Key) })
		candidates := make([]operation.SelectionCandidate, 0, len(matches))
		for _, model := range matches {
			candidates = append(candidates, operation.SelectionCandidate{Key: model.Key, Name: model.Name, Base: model.Base, Type: model.Type})
		}
		return ModelIdentifier{}, operation.SelectionRequired(kind, selector, candidates)
	}
	return ModelIdentifier{}, operation.InvalidRequest(fmt.Sprintf("model selector %q did not resolve to an installed model", selector))
}

func (m modelRecord) identifier() (ModelIdentifier, error) {
	if m.Key == "" || m.Hash == "" || m.Name == "" || m.Base == "" || m.Type == "" {
		return ModelIdentifier{}, operation.UnsupportedCapability(fmt.Sprintf("installed model %q does not provide a complete InvokeAI model identifier", m.Key))
	}
	return ModelIdentifier{Key: m.Key, Hash: m.Hash, Name: m.Name, Base: m.Base, Type: m.Type}, nil
}
