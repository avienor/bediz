// Package directexecution owns the lifecycle of graph-producing operations.
package directexecution

import (
	"context"
	"time"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
)

// Options controls local waiting without changing the operation request.
type Options struct {
	NoWait  bool
	Timeout time.Duration
}

// An adapter supplies operation-specific resolution, compilation, receipt,
// Recall, and completion checks. Lifecycle ordering belongs to execution.
type adapter[Receipt any] interface {
	prepare() (*sourceimage.Prepared, error)
	resolve([]graphops.ModelIdentifier) (capability.Entry, []result.Warning, error)
	compile(sourceimage.Resolved) (graphops.EnqueueRequest, int, error)
	receipt(graphops.QueueReceipt, sourceimage.Resolved, []result.Warning) Receipt
	synchronize(context.Context, *httpclient.Client, Receipt) Receipt
	wait(context.Context, *httpclient.Client, Receipt, graphops.WaitOptions) (Receipt, error)
}

type execution[Receipt any] struct {
	adapter adapter[Receipt]
}

func (e execution[Receipt]) run(ctx context.Context, client *httpclient.Client, options Options) (receipt Receipt, err error) {
	if options.Timeout < 0 {
		return receipt, operation.InvalidRequest("timeout cannot be negative")
	}
	if options.NoWait && options.Timeout > 0 {
		return receipt, operation.InvalidRequest("--timeout cannot be combined with --no-wait")
	}
	var warnings []result.Warning
	var source sourceimage.Resolved
	accepted := false
	defer func() {
		if err == nil {
			return
		}
		if !accepted && len(warnings) > 0 {
			err = &operation.ProfilePreferenceError{Err: err, Warnings: warnings}
		}
		if source.Uploaded {
			err = &sourceimage.UploadedError{Source: source.Image, Err: err}
		}
		// A named upload with an invalid URL already carries UploadedError.
		// Its outer preference warnings stay outside that error, preserving
		// the Structured Error contract's warning omission for this case.
	}()
	prepared, err := e.adapter.prepare()
	if prepared != nil {
		defer func() { _ = prepared.Close() }()
	}
	if err != nil {
		return receipt, err
	}
	if err := capability.RequireSupportedVersion(ctx, client); err != nil {
		return receipt, err
	}
	inventory, err := graphops.Inventory(ctx, client)
	if err != nil {
		return receipt, err
	}
	var entry capability.Entry
	entry, warnings, err = e.adapter.resolve(inventory)
	if err != nil {
		return receipt, err
	}
	if err := graphops.CheckEntry(ctx, client, entry); err != nil {
		return receipt, err
	}
	if prepared != nil {
		source, err = prepared.Resolve(ctx, client)
		if err != nil {
			return receipt, err
		}
	}
	request, expectedItems, err := e.adapter.compile(source)
	if err != nil {
		return receipt, err
	}
	queue, err := graphops.Enqueue(ctx, client, request, expectedItems)
	if err != nil {
		return receipt, err
	}
	accepted = true
	receipt = e.adapter.receipt(queue, source, warnings)
	receipt = e.adapter.synchronize(ctx, client, receipt)
	if options.NoWait {
		return receipt, nil
	}
	return e.adapter.wait(ctx, client, receipt, graphops.WaitOptions{Timeout: options.Timeout})
}
