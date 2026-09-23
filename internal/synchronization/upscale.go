package synchronization

import (
	"context"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/recall"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/upscale"
)

// upscaleNotRestored lists the Upscale tab controls stock InvokeAI 6.14.1
// Recall cannot set. The patch carries only controls the Upscale tab shares
// with the Generate tab; width, height, and cfg_scale are never sent because
// dimensions do not apply to the Upscale panel and Recall writes cfg_scale to
// the Generate tab's CFG instead of the upscale CFG.
var upscaleNotRestored = []string{
	"source_image", "upscale_model", "scale", "creativity", "structure", "tile_controlnet",
	"tile_size", "tile_overlap", "scheduler", "guidance", "vae", "board_id",
}

// SynchronizeUpscale attempts one Recall patch for an accepted upscale. Its
// failure adds a warning without changing the upscale outcome or the single enqueue.
func SynchronizeUpscale(ctx context.Context, client *httpclient.Client, receipt upscale.ExecutionReceipt) upscale.ExecutionReceipt {
	settings := receipt.ResolvedSettings
	patch := recall.Request{
		SchemaVersion:  1,
		Model:          new(settings.ModelKey),
		PositivePrompt: new(settings.PositivePrompt),
		NegativePrompt: new(settings.NegativePrompt),
		Steps:          new(settings.Steps),
		Seed:           new(settings.Seeds[0]),
	}
	if _, err := recall.SubmitWithMainResolver(ctx, client, patch, upscale.ResolveMain); err != nil {
		receipt.Warnings = append(receipt.Warnings, result.Warning{
			Code: "ui_sync_failed", Message: "Upscale was accepted, but the UI Recall patch could not be confirmed.",
		})
		return receipt
	}
	receipt.Warnings = append(receipt.Warnings, result.Warning{
		Code:    "ui_sync_partial",
		Message: "InvokeAI accepted the Recall patch; stock InvokeAI does not restore all upscale controls.",
		Details: map[string]any{"not_restored": upscaleNotRestored},
	})
	return receipt
}
