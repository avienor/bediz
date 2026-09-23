package generation

import (
	"context"
	"fmt"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type WaitOptions = graphops.WaitOptions

// Wait completes a generation receipt using read-only queue inspection.
func Wait(ctx context.Context, client *httpclient.Client, accepted ExecutionReceipt, options WaitOptions) (ExecutionReceipt, error) {
	if options.Timeout < 0 {
		return accepted, operation.InvalidRequest("wait timeout cannot be negative")
	}
	if len(accepted.Queue.ItemIDs) == 0 || len(accepted.Queue.ItemIDs) != len(accepted.ResolvedSettings.Seeds) ||
		len(accepted.Queue.ItemIDs) != accepted.ResolvedSettings.OutputCount {
		return accepted, operation.InvalidRequest(fmt.Sprintf(
			"wait requires one item and seed per resolved output: %d outputs, %d queue items, and %d resolved seeds",
			accepted.ResolvedSettings.OutputCount, len(accepted.Queue.ItemIDs), len(accepted.ResolvedSettings.Seeds),
		))
	}
	outputs, err := graphops.Wait(ctx, client, accepted.Queue, accepted.ResolvedSettings.Seeds,
		graphops.SeedField{NodePath: "seed", FieldName: "value"}, options)
	accepted.Outputs = outputs
	return accepted, err
}
