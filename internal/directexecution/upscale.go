package directexecution

import (
	"context"
	"crypto/rand"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/recall"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
	"github.com/avienor/bediz/internal/upscale"
)

// Upscale runs the complete upscale lifecycle from the typed request to its
// accepted or completed Execution Receipt.
func Upscale(ctx context.Context, client *httpclient.Client, request upscale.Request, options Options) (upscale.ExecutionReceipt, error) {
	return (execution[upscale.ExecutionReceipt]{adapter: &upscaleAdapter{request: request}}).run(ctx, client, options)
}

type upscaleAdapter struct {
	request     upscale.Request
	preparation *upscale.Preparation
}

func (a *upscaleAdapter) prepare() (*sourceimage.Prepared, error) {
	preparation, err := upscale.Prepare(a.request)
	a.preparation = preparation
	if preparation == nil {
		return nil, err
	}
	return preparation.Source(), err
}

func (a *upscaleAdapter) resolve(inventory []graphops.ModelIdentifier) (capability.Entry, []result.Warning, error) {
	return a.preparation.Resolve(inventory, rand.Reader)
}

func (a *upscaleAdapter) compile(source sourceimage.Resolved) (graphops.EnqueueRequest, int, error) {
	return a.preparation.Compile(source)
}

func (a *upscaleAdapter) receipt(queue graphops.QueueReceipt, source sourceimage.Resolved, warnings []result.Warning) upscale.ExecutionReceipt {
	return a.preparation.Receipt(queue, source, warnings)
}

func (*upscaleAdapter) synchronize(ctx context.Context, client *httpclient.Client, receipt upscale.ExecutionReceipt) upscale.ExecutionReceipt {
	settings := receipt.ResolvedSettings
	patch := recall.Request{
		SchemaVersion:  1,
		Model:          new(settings.ModelKey),
		PositivePrompt: new(settings.PositivePrompt),
		NegativePrompt: new(settings.NegativePrompt),
		Steps:          new(settings.Steps),
		Seed:           new(settings.Seeds[0]),
	}
	if _, err := recall.SubmitSynchronization(ctx, client, recall.SynchronizationRequest{Patch: patch, ResolveMain: upscale.ResolveMain}); err != nil {
		receipt.Warnings = append(receipt.Warnings, result.Warning{
			Code: "ui_sync_failed", Message: "Upscale was accepted, but the UI Recall patch could not be confirmed.",
		})
		return receipt
	}
	receipt.Warnings = append(receipt.Warnings, result.Warning{
		Code:    "ui_sync_partial",
		Message: "InvokeAI accepted the Recall patch; stock InvokeAI does not restore all upscale controls.",
		Details: map[string]any{"not_restored": []string{
			"source_image", "upscale_model", "scale", "creativity", "structure", "tile_controlnet",
			"tile_size", "tile_overlap", "scheduler", "guidance", "vae", "board_id",
		}},
	})
	return receipt
}

func (*upscaleAdapter) wait(ctx context.Context, client *httpclient.Client, receipt upscale.ExecutionReceipt, options graphops.WaitOptions) (upscale.ExecutionReceipt, error) {
	return upscale.Wait(ctx, client, receipt, options)
}
