package graphops

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/queue"
)

// WaitOptions bounds local waiting. A zero Timeout waits without a total
// deadline.
type WaitOptions struct {
	Timeout                   time.Duration
	OnlyNonIntermediateImages bool
}

// Wait polls accepted queue items with the intervals and tested status set of
// queue waiting. It returns completed outputs accumulated before
// any error. A timeout or interruption stops only local waiting.
func Wait(ctx context.Context, client *httpclient.Client, accepted QueueReceipt, seeds []uint32, seedField SeedField, options WaitOptions) ([]Output, error) {
	if options.Timeout < 0 {
		return nil, operation.InvalidRequest("wait timeout cannot be negative")
	}
	if len(accepted.ItemIDs) == 0 || len(accepted.ItemIDs) != len(seeds) {
		return nil, operation.InvalidRequest(fmt.Sprintf(
			"wait requires one item and seed per resolved output: %d outputs, %d queue items, and %d resolved seeds",
			len(seeds), len(accepted.ItemIDs), len(seeds),
		))
	}
	position := operation.QueuePosition{QueueID: accepted.QueueID, BatchID: accepted.BatchID, ItemIDs: accepted.ItemIDs}
	waitContext := ctx
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		waitContext, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	outputs := []Output{}
	for index, itemID := range accepted.ItemIDs {
		item, err := waitForItem(ctx, waitContext, client, position, itemID)
		if err != nil {
			return outputs, err
		}
		output, err := completedOutput(position, itemID, seeds[index], seedField, item, options.OnlyNonIntermediateImages)
		if err != nil {
			return outputs, err
		}
		outputs = append(outputs, output)
	}
	return outputs, nil
}

func waitForItem(ctx, waitContext context.Context, client *httpclient.Client, position operation.QueuePosition, itemID int) (queue.Item, error) {
	for interval := queue.FirstPollInterval; ; interval = min(interval*2, queue.MaxPollInterval) {
		result, err := queue.Get(waitContext, client, queue.GetRequest{
			SchemaVersion: 1,
			QueueID:       position.QueueID,
			ItemID:        itemID,
		})
		if err != nil {
			return queue.Item{}, waitStopped(ctx, waitContext, position, err)
		}
		item := result.Item
		if item.ItemID != itemID || item.QueueID != position.QueueID || item.BatchID != position.BatchID {
			return queue.Item{}, &operation.InvalidQueueResultError{
				Position: position, ItemID: itemID, Status: item.Status,
				Detail: fmt.Sprintf("reported contradictory queue identity: item %d, queue %q, batch %q", item.ItemID, item.QueueID, item.BatchID),
			}
		}
		switch item.Status {
		case queue.StatusPending, queue.StatusInProgress, queue.StatusWaiting:
		case queue.StatusCompleted:
			return item, nil
		case queue.StatusFailed, queue.StatusCanceled:
			failureType, failureMessage := normalizedFailure(item)
			return queue.Item{}, &operation.ItemFailureError{
				Position: position, ItemID: itemID, Status: item.Status,
				FailureType: failureType, FailureMessage: failureMessage,
			}
		default:
			return queue.Item{}, &operation.InvalidQueueResultError{
				Position: position, ItemID: itemID, Status: item.Status,
				Detail: fmt.Sprintf("reported status %q, which is not a tested InvokeAI queue status", item.Status),
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-waitContext.Done():
			timer.Stop()
			return queue.Item{}, waitStopped(ctx, waitContext, position, nil)
		case <-timer.C:
		}
	}
}

// completedOutput verifies the batch metadata before associating an image
// with the same-position resolved seed.
func completedOutput(position operation.QueuePosition, itemID int, expectedSeed uint32, seedField SeedField, item queue.Item, onlyNonIntermediate bool) (Output, error) {
	seed, err := itemSeed(item, seedField)
	if err != nil {
		return Output{}, &operation.InvalidQueueResultError{
			Position: position, ItemID: itemID, Status: item.Status, Detail: err.Error(),
		}
	}
	if seed != expectedSeed {
		return Output{}, &operation.InvalidQueueResultError{
			Position: position, ItemID: itemID, Status: item.Status,
			Detail: fmt.Sprintf("batch metadata seed %d contradicts resolved seed %d", seed, expectedSeed),
		}
	}
	if item.ImageOutputValidationError != "" {
		return Output{}, &operation.InvalidQueueResultError{
			Position: position, ItemID: itemID, Status: item.Status, Detail: item.ImageOutputValidationError,
		}
	}
	if onlyNonIntermediate {
		if item.NonIntermediateImageOutputCount != 1 {
			return Output{}, &operation.InvalidQueueResultError{
				Position: position, ItemID: itemID, Status: item.Status,
				Detail: fmt.Sprintf("completed with %d non-intermediate image outputs; expected exactly one", item.NonIntermediateImageOutputCount),
			}
		}
		var finalImages []images.Reference
		for _, image := range item.Images {
			if !image.IsIntermediate {
				finalImages = append(finalImages, image)
			}
		}
		if len(finalImages) != 1 {
			return Output{}, &operation.InvalidQueueResultError{
				Position: position, ItemID: itemID, Status: item.Status,
				Detail: fmt.Sprintf("completed with one non-intermediate image output but %d accessible final Image References", len(finalImages)),
			}
		}
		return Output{ItemID: itemID, Seed: expectedSeed, Image: finalImages[0]}, nil
	}
	if item.ImageOutputCount != 1 {
		return Output{}, &operation.InvalidQueueResultError{
			Position: position, ItemID: itemID, Status: item.Status,
			Detail: fmt.Sprintf("completed with %d image outputs; expected exactly one", item.ImageOutputCount),
		}
	}
	if len(item.Images) != 1 {
		return Output{}, &operation.InvalidQueueResultError{
			Position: position, ItemID: itemID, Status: item.Status,
			Detail: fmt.Sprintf("completed with one image output but %d accessible Image References", len(item.Images)),
		}
	}
	return Output{ItemID: itemID, Seed: expectedSeed, Image: item.Images[0]}, nil
}

func itemSeed(item queue.Item, seedField SeedField) (uint32, error) {
	var seed *uint32
	for _, fieldValue := range item.FieldValues {
		if fieldValue.NodePath != seedField.NodePath || fieldValue.FieldName != seedField.FieldName {
			continue
		}
		if seed != nil {
			return 0, errors.New("batch metadata contains several seed values")
		}
		var value uint32
		if err := json.Unmarshal(fieldValue.Value, &value); err != nil {
			return 0, fmt.Errorf("batch metadata contains an invalid seed: %w", err)
		}
		seed = new(value)
	}
	if seed == nil {
		return 0, errors.New("batch metadata is missing the resolved seed")
	}
	return *seed, nil
}

// normalizedFailure returns the concise failure InvokeAI reports for a failed
// item. A server traceback is never part of the normalized queue item.
func normalizedFailure(item queue.Item) (failureType, failureMessage string) {
	if item.Error == nil {
		return "", ""
	}
	return item.Error.Type, item.Error.Message
}

// waitStopped reports why local waiting ended. A canceled caller context is a
// local interruption, an elapsed wait deadline is a timeout, and otherwise the
// failed read is returned unchanged.
func waitStopped(ctx, waitContext context.Context, position operation.QueuePosition, err error) error {
	if ctx.Err() != nil {
		return &operation.InterruptedError{Position: position}
	}
	if errors.Is(waitContext.Err(), context.DeadlineExceeded) {
		return &operation.WaitTimeoutError{Position: position}
	}
	return err
}
