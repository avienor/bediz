package generation

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
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
	Strength       *float64          `json:"strength,omitempty"`
}

type QueueReceipt = graphops.QueueReceipt
type Output = graphops.Output

type ExecutionReceipt struct {
	Family           string            `json:"-"`
	SubmittedRequest Request           `json:"submitted_request"`
	ResolvedSettings ResolvedSettings  `json:"resolved_settings"`
	Queue            QueueReceipt      `json:"queue"`
	Outputs          []Output          `json:"outputs"`
	Warnings         []result.Warning  `json:"warnings"`
	SourceImage      *images.Reference `json:"source_image,omitempty"`
	SourceUploaded   *bool             `json:"source_uploaded,omitempty"`
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
	var prepared *sourceimage.Prepared
	var localWidth, localHeight int
	if request.Source != nil {
		var err error
		prepared, err = sourceimage.Prepare(*request.Source)
		if err != nil {
			return ExecutionReceipt{}, err
		}
		defer func() { _ = prepared.Close() }()
		if request.Source.Type == "path" {
			localWidth, localHeight, err = prepared.LocalDimensions()
			if err != nil {
				return ExecutionReceipt{}, err
			}
		}
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
	if effective.Source != nil && main.Base != "sdxl" && main.Base != "anima" {
		return ExecutionReceipt{}, operation.UnsupportedCapability(fmt.Sprintf("image-to-image is not supported for %s main models", main.Base))
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
	if effective.Source != nil {
		if effective.Strength == nil {
			effective.Strength = new(0.75)
		}
		if request.Source.Type == "path" {
			if err := checkSourceSize(localWidth, localHeight); err != nil {
				return ExecutionReceipt{}, err
			}
			if effective.Width == nil {
				effective.Width, effective.Height = new(localWidth/8*8), new(localHeight/8*8)
			}
		}
	}
	resolved, err := adapter.resolve(effective, main, inventory, rand.Reader)
	if err != nil {
		return ExecutionReceipt{}, err
	}
	if effective.Source != nil {
		entry := capability.SDXLImageToImageEntry()
		if main.Base == "anima" {
			entry = capability.AnimaImageToImageEntry()
		}
		if err := graphops.CheckRequirements(ctx, client, entry.Endpoints, entry.Invocations); err != nil {
			return ExecutionReceipt{}, err
		}
	} else if err := graphops.CheckInvocations(ctx, client, adapter.invocations()); err != nil {
		return ExecutionReceipt{}, err
	}
	var source sourceimage.Resolved
	if prepared != nil {
		source, err = prepared.Resolve(ctx, client)
		if err != nil {
			return ExecutionReceipt{}, err
		}
		if source.Uploaded && (source.Image.Width != localWidth || source.Image.Height != localHeight) {
			return ExecutionReceipt{}, &sourceimage.UploadedError{Source: source.Image, Err: &httpclient.InvalidResponseError{Err: fmt.Errorf("uploaded source dimensions %d × %d differ from local image dimensions %d × %d", source.Image.Width, source.Image.Height, localWidth, localHeight)}}
		}
		if err := checkSourceSize(source.Image.Width, source.Image.Height); err != nil {
			return ExecutionReceipt{}, err
		}
		if request.Width == nil {
			resolved.Request.Width, resolved.Request.Height = new(source.Image.Width/8*8), new(source.Image.Height/8*8)
		}
		resolved.SourceImage = source.Image
	}
	enqueueRequest, err := adapter.compile(resolved)
	if err != nil {
		if source.Uploaded {
			return ExecutionReceipt{}, &sourceimage.UploadedError{Source: source.Image, Err: err}
		}
		return ExecutionReceipt{}, err
	}
	queueReceipt, err := graphops.Enqueue(ctx, client, enqueueRequest, *resolved.Request.OutputCount)
	if err != nil {
		if source.Uploaded {
			return ExecutionReceipt{}, &sourceimage.UploadedError{Source: source.Image, Err: err}
		}
		return ExecutionReceipt{}, err
	}

	receipt := ExecutionReceipt{
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
			Strength:       resolved.Request.Strength,
		},
		Queue:    queueReceipt,
		Outputs:  []Output{},
		Warnings: append([]result.Warning{}, profileWarnings...),
	}
	if prepared != nil {
		receipt.SourceImage = &source.Image
		receipt.SourceUploaded = new(source.Uploaded)
	}
	return receipt, nil
}

func checkSourceSize(width, height int) error {
	if width < 8 || height < 8 {
		return operation.InvalidRequest("source image width and height must each be at least 8 pixels")
	}
	return nil
}
