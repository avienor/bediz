package synchronization

import (
	"context"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/recall"
	"github.com/avienor/bediz/internal/result"
)

// Synchronize attempts Recall only for an accepted generation. Recall is
// independent of the queued work: its failure adds a warning without changing
// resolved settings, the generation outcome, or the single enqueue.
func Synchronize(ctx context.Context, client *httpclient.Client, receipt generation.ExecutionReceipt) generation.ExecutionReceipt {
	return synchronizeFamily(ctx, client, receipt, receipt.Family)
}

// SynchronizeAnima preserves the Anima-specific public entry point.
func SynchronizeAnima(ctx context.Context, client *httpclient.Client, receipt generation.ExecutionReceipt) generation.ExecutionReceipt {
	return synchronizeFamily(ctx, client, receipt, "anima")
}

func synchronizeFamily(ctx context.Context, client *httpclient.Client, receipt generation.ExecutionReceipt, family string) generation.ExecutionReceipt {
	settings, notRestored, err := generation.SynchronizationFields(family, receipt.ResolvedSettings)
	if err != nil {
		return syncFailure(receipt)
	}
	patch := recall.Request{
		SchemaVersion:  1,
		Model:          new(settings.Model),
		PositivePrompt: new(settings.PositivePrompt),
		NegativePrompt: new(settings.NegativePrompt),
		Width:          new(settings.Width),
		Height:         new(settings.Height),
		Steps:          new(settings.Steps),
		Seed:           new(settings.Seed),
	}
	if _, err := recall.SubmitWithFields(ctx, client, patch, settings.Additional); err != nil {
		return syncFailure(receipt)
	}
	receipt.Warnings = append(receipt.Warnings, result.Warning{
		Code:    "ui_sync_partial",
		Message: "InvokeAI accepted the Recall patch; stock InvokeAI does not restore all generation controls.",
		Details: map[string]any{"not_restored": notRestored},
	})
	return receipt
}

func syncFailure(receipt generation.ExecutionReceipt) generation.ExecutionReceipt {
	receipt.Warnings = append(receipt.Warnings, result.Warning{
		Code: "ui_sync_failed", Message: "Generation was accepted, but the UI Recall patch could not be confirmed.",
	})
	return receipt
}
