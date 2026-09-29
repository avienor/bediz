package directexecution

import (
	"context"
	"crypto/rand"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
)

// Generate runs the complete generation lifecycle from the typed request to
// its accepted or completed Execution Receipt.
func Generate(ctx context.Context, client *httpclient.Client, request generation.Request, options Options) (generation.ExecutionReceipt, error) {
	return (execution[generation.ExecutionReceipt]{adapter: &generateAdapter{request: request}}).run(ctx, client, options)
}

type generateAdapter struct {
	request     generation.Request
	preparation *generation.Preparation
}

func (a *generateAdapter) prepare() (*sourceimage.Prepared, error) {
	preparation, err := generation.Prepare(a.request)
	a.preparation = preparation
	if preparation == nil {
		return nil, err
	}
	return preparation.Source(), err
}

func (a *generateAdapter) resolve(inventory []graphops.ModelIdentifier) (capability.Entry, []result.Warning, error) {
	return a.preparation.Resolve(inventory, rand.Reader)
}

func (a *generateAdapter) compile(source sourceimage.Resolved) (graphops.EnqueueRequest, int, error) {
	return a.preparation.Compile(source)
}

func (a *generateAdapter) receipt(queue graphops.QueueReceipt, source sourceimage.Resolved, warnings []result.Warning) generation.ExecutionReceipt {
	return a.preparation.Receipt(queue, source, warnings)
}

func (*generateAdapter) synchronize(ctx context.Context, client *httpclient.Client, receipt generation.ExecutionReceipt) generation.ExecutionReceipt {
	return Synchronize(ctx, client, receipt)
}

func (*generateAdapter) wait(ctx context.Context, client *httpclient.Client, receipt generation.ExecutionReceipt, options graphops.WaitOptions) (generation.ExecutionReceipt, error) {
	return generation.Wait(ctx, client, receipt, options)
}
