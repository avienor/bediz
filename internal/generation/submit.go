package generation

import (
	"context"
	"crypto/rand"
	"slices"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
)

type ResolvedSettings struct {
	Profile        string            `json:"profile,omitempty"`
	PositivePrompt string            `json:"positive_prompt"`
	NegativePrompt string            `json:"negative_prompt"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	Steps          int               `json:"steps"`
	Scheduler      string            `json:"scheduler"`
	Guidance       *float64          `json:"guidance,omitempty"`
	OutputCount    int               `json:"output_count"`
	BoardID        string            `json:"board_id,omitempty"`
	ModelKey       string            `json:"model_key"`
	ComponentKeys  map[string]string `json:"component_keys"`
	Seeds          []uint32          `json:"seeds"`
}

type QueueReceipt = graphops.QueueReceipt
type Output = graphops.Output

type ExecutionReceipt struct {
	Family           string           `json:"-"`
	SubmittedRequest Request          `json:"submitted_request"`
	ResolvedSettings ResolvedSettings `json:"resolved_settings"`
	Queue            QueueReceipt     `json:"queue"`
	Outputs          []Output         `json:"outputs"`
	Warnings         []result.Warning `json:"warnings"`
}

func Submit(ctx context.Context, client *httpclient.Client, request Request) (ExecutionReceipt, error) {
	if err := validateCommonRequest(request); err != nil {
		return ExecutionReceipt{}, err
	}
	effective := request
	var profileGenerate *profiles.Generate
	if request.Profile != "" {
		if !profiles.ValidName(request.Profile) {
			return ExecutionReceipt{}, operation.InvalidRequest("invalid profile name")
		}
		profile, err := profiles.Get(request.Profile)
		if err != nil {
			return ExecutionReceipt{}, profileLoadError(request.Profile, err)
		}
		if profile.Generate == nil {
			return ExecutionReceipt{}, operation.InvalidRequest("profile has no generate section")
		}
		profileGenerate = profile.Generate
		if effective.Model == "" && profileGenerate.Model != nil {
			effective.Model = *profileGenerate.Model
		}
	}
	if effective.Model == "" {
		return ExecutionReceipt{}, operation.InvalidRequest("model is required")
	}
	if err := capability.RequireSupportedVersion(ctx, client); err != nil {
		return ExecutionReceipt{}, err
	}
	inventory, err := graphops.Inventory(ctx, client)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	main, err := ResolveFamilyMain(inventory, effective.Model)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	if profileGenerate != nil {
		if err := validateProfileApplicability(request.Profile, *profileGenerate, main); err != nil {
			return ExecutionReceipt{}, err
		}
		effective = applyProfileSettings(effective, *profileGenerate)
	}
	var profileWarnings []result.Warning
	if profileGenerate != nil {
		effective, profileWarnings = applyProfileComponents(effective, *profileGenerate, main, inventory)
	}
	adapter, err := adapterForBase(main.Base)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	if err := graphops.CheckInvocations(ctx, client, adapter.invocations()); err != nil {
		return ExecutionReceipt{}, err
	}
	resolved, err := adapter.resolve(effective, main, inventory, rand.Reader)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	enqueueRequest, err := adapter.compile(resolved)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	queueReceipt, err := graphops.Enqueue(ctx, client, enqueueRequest, *resolved.Request.OutputCount)
	if err != nil {
		return ExecutionReceipt{}, err
	}

	return ExecutionReceipt{
		Family:           main.Base,
		SubmittedRequest: request,
		ResolvedSettings: ResolvedSettings{
			Profile:        request.Profile,
			PositivePrompt: resolved.Request.PositivePrompt,
			NegativePrompt: resolved.Request.NegativePrompt,
			Width:          *resolved.Request.Width,
			Height:         *resolved.Request.Height,
			Steps:          *resolved.Request.Steps,
			Scheduler:      *resolved.Request.Scheduler,
			Guidance:       resolved.Request.Guidance,
			OutputCount:    *resolved.Request.OutputCount,
			BoardID:        resolved.Request.BoardID,
			ModelKey:       resolved.Models.Main.Key,
			ComponentKeys:  adapter.componentKeys(resolved),
			Seeds:          slices.Clone(resolved.Seeds),
		},
		Queue:    queueReceipt,
		Outputs:  []Output{},
		Warnings: append([]result.Warning{}, profileWarnings...),
	}, nil
}
