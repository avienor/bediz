package synchronization

import (
	"context"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/recall"
	"github.com/avienor/bediz/internal/result"
)

// SynchronizeAnima attempts Recall only for an accepted generation. Recall is
// independent of the queued work: its failure adds a warning without changing
// resolved settings, the generation outcome, or the single enqueue.
func SynchronizeAnima(ctx context.Context, client *httpclient.Client, receipt generation.ExecutionReceipt) generation.ExecutionReceipt {
	settings := receipt.ResolvedSettings
	patch := recall.Request{
		SchemaVersion:  1,
		Model:          new(settings.ModelKey),
		PositivePrompt: new(settings.PositivePrompt),
		NegativePrompt: new(settings.NegativePrompt),
		Width:          new(settings.Width),
		Height:         new(settings.Height),
		Steps:          new(settings.Steps),
		Seed:           new(settings.Seeds[0]),
	}
	if _, err := recall.Submit(ctx, client, patch); err != nil {
		receipt.Warnings = []result.Warning{{
			Code: "ui_sync_failed", Message: "Generation was accepted, but the UI Recall patch could not be confirmed.",
		}}
		return receipt
	}
	receipt.Warnings = []result.Warning{{
		Code:    "ui_sync_partial",
		Message: "InvokeAI accepted the Recall patch; stock InvokeAI does not restore all generation controls.",
		Details: map[string]any{"not_restored": []string{
			"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id",
		}},
	}}
	return receipt
}
