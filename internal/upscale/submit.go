package upscale

import (
	"context"
	"fmt"
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
	Profile        string            `json:"profile,omitempty"`
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
