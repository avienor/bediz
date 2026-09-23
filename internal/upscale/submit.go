package upscale

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"slices"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/result"
)

type QueueReceipt = graphops.QueueReceipt
type Output = graphops.Output
type WaitOptions = graphops.WaitOptions

type ResolvedSettings struct {
	PositivePrompt string            `json:"positive_prompt"`
	NegativePrompt string            `json:"negative_prompt"`
	Scale          int               `json:"scale"`
	Creativity     int               `json:"creativity"`
	Structure      int               `json:"structure"`
	Steps          int               `json:"steps"`
	Scheduler      string            `json:"scheduler"`
	Guidance       float64           `json:"guidance"`
	TileSize       int               `json:"tile_size"`
	TileOverlap    int               `json:"tile_overlap"`
	OutputWidth    int               `json:"output_width"`
	OutputHeight   int               `json:"output_height"`
	BoardID        string            `json:"board_id,omitempty"`
	ModelKey       string            `json:"model_key"`
	ComponentKeys  map[string]string `json:"component_keys"`
	Seeds          []uint32          `json:"seeds"`
}

type ExecutionReceipt struct {
	SubmittedRequest Request          `json:"submitted_request"`
	SourceImage      images.Reference `json:"source_image"`
	SourceUploaded   bool             `json:"source_uploaded"`
	ResolvedSettings ResolvedSettings `json:"resolved_settings"`
	Queue            QueueReceipt     `json:"queue"`
	Outputs          []Output         `json:"outputs"`
	Warnings         []result.Warning `json:"warnings"`
}

// Submit validates the complete operation, then sends one enqueue mutation.
func Submit(ctx context.Context, client *httpclient.Client, request Request) (ExecutionReceipt, error) {
	if err := ValidateRequest(request); err != nil {
		return ExecutionReceipt{}, err
	}
	if err := graphops.CheckVersion(ctx, client); err != nil {
		return ExecutionReceipt{}, err
	}
	inventory, err := graphops.Inventory(ctx, client)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	resolved, err := Resolve(request, inventory, rand.Reader)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	family, _ := familyFor(resolved.Models.Main.Base)
	if err := graphops.CheckInvocations(ctx, client, family.entry().Invocations); err != nil {
		return ExecutionReceipt{}, err
	}
	imageResult, err := images.Get(ctx, client, images.GetRequest{SchemaVersion: 1, ImageName: request.Source.Reference})
	if err != nil {
		return ExecutionReceipt{}, err
	}
	source := imageResult.Image
	if source.ImageName != request.Source.Reference {
		return ExecutionReceipt{}, &httpclient.InvalidResponseError{Err: fmt.Errorf("source image has contradictory name %q; expected %q", source.ImageName, request.Source.Reference)}
	}
	if source.Width < 1 || source.Height < 1 || source.Width > math.MaxInt / *resolved.Request.Scale || source.Height > math.MaxInt / *resolved.Request.Scale {
		return ExecutionReceipt{}, &httpclient.InvalidResponseError{Err: fmt.Errorf("source image has invalid dimensions %d × %d", source.Width, source.Height)}
	}
	outputWidth := source.Width * *resolved.Request.Scale / 8 * 8
	outputHeight := source.Height * *resolved.Request.Scale / 8 * 8
	graph, err := Compile(resolved, source)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	queueReceipt, err := graphops.Enqueue(ctx, client, graph, 1)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	componentKeys := map[string]string{
		"upscale_model":   resolved.Models.UpscaleModel.Key,
		"tile_controlnet": resolved.Models.TileControlNet.Key,
	}
	if resolved.Models.VAE.Key != "" {
		componentKeys["vae"] = resolved.Models.VAE.Key
	}
	return ExecutionReceipt{
		SubmittedRequest: request, SourceImage: source, SourceUploaded: false,
		ResolvedSettings: ResolvedSettings{
			PositivePrompt: resolved.Request.PositivePrompt, NegativePrompt: resolved.Request.NegativePrompt,
			Scale: *resolved.Request.Scale, Creativity: *resolved.Request.Creativity, Structure: *resolved.Request.Structure,
			Steps: *resolved.Request.Steps, Scheduler: *resolved.Request.Scheduler, Guidance: *resolved.Request.Guidance,
			TileSize: *resolved.Request.TileSize, TileOverlap: *resolved.Request.TileOverlap,
			OutputWidth: outputWidth, OutputHeight: outputHeight, BoardID: resolved.Request.BoardID,
			ModelKey: resolved.Models.Main.Key, ComponentKeys: componentKeys, Seeds: []uint32{resolved.Seed},
		},
		Queue: queueReceipt, Outputs: []Output{}, Warnings: []result.Warning{},
	}, nil
}

// ScaleNotAppliedError means InvokeAI completed an image that does not match
// the expected scale. The failed output is retained for inspection, but it is
// never returned in a successful execution receipt.
type ScaleNotAppliedError struct {
	Queue          QueueReceipt
	SourceImage    images.Reference
	Output         Output
	ExpectedWidth  int
	ExpectedHeight int
	ActualWidth    int
	ActualHeight   int
}

func (e *ScaleNotAppliedError) Error() string {
	return fmt.Sprintf("upscale output dimensions %d × %d do not match expected %d × %d", e.ActualWidth, e.ActualHeight, e.ExpectedWidth, e.ExpectedHeight)
}

// Wait inspects the accepted queue item, checks its seed and output image, and
// reports a scale failure when the completed dimensions differ from the receipt.
func Wait(ctx context.Context, client *httpclient.Client, accepted ExecutionReceipt, options WaitOptions) (ExecutionReceipt, error) {
	if len(accepted.Queue.ItemIDs) != 1 || len(accepted.ResolvedSettings.Seeds) != 1 {
		return accepted, operation.InvalidRequest("upscale wait requires one accepted queue item and seed")
	}
	options.OnlyNonIntermediateImages = true
	outputs, err := graphops.Wait(ctx, client, accepted.Queue, accepted.ResolvedSettings.Seeds,
		graphops.SeedField{NodePath: "seed", FieldName: "value"}, options)
	if err != nil {
		return accepted, err
	}
	output := outputs[0]
	if output.Image.Width != accepted.ResolvedSettings.OutputWidth || output.Image.Height != accepted.ResolvedSettings.OutputHeight {
		return accepted, &ScaleNotAppliedError{
			Queue: accepted.Queue, SourceImage: accepted.SourceImage, Output: output,
			ExpectedWidth: accepted.ResolvedSettings.OutputWidth, ExpectedHeight: accepted.ResolvedSettings.OutputHeight,
			ActualWidth: output.Image.Width, ActualHeight: output.Image.Height,
		}
	}
	accepted.Outputs = slices.Clone(outputs)
	return accepted, nil
}
