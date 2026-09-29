package generation

import (
	"fmt"
	"io"
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

// Preparation holds generation-specific validation and resolution state. Direct
// Execution owns the network lifecycle and closes its prepared Source Image.
type Preparation struct {
	request         Request
	effective       Request
	profileGenerate *profiles.Generate
	source          *sourceimage.Prepared
	localWidth      int
	localHeight     int
	adapter         familyAdapter
	resolved        Resolution
}

// Prepare validates the request and profile and checks a local Source Image
// before network access. A non-nil preparation must be closed through Source,
// even when reading the local image dimensions fails.
func Prepare(request Request) (*Preparation, error) {
	if err := validateCommonRequest(request); err != nil {
		return nil, err
	}
	effective := request
	var profileGenerate *profiles.Generate
	if request.Profile != "" {
		if !profiles.ValidName(request.Profile) {
			return nil, operation.InvalidRequest("invalid profile name")
		}
		profile, err := profiles.Get(request.Profile)
		if err != nil {
			return nil, profileLoadError(request.Profile, err)
		}
		if profile.Generate == nil {
			return nil, operation.InvalidRequest("profile has no generate section")
		}
		profileGenerate = profile.Generate
		if effective.Model == "" && profileGenerate.Model != nil {
			effective.Model = *profileGenerate.Model
		}
	}
	if effective.Model == "" {
		return nil, operation.InvalidRequest("model is required")
	}
	preparation := &Preparation{request: request, effective: effective, profileGenerate: profileGenerate}
	if request.Source != nil {
		var err error
		preparation.source, err = sourceimage.Prepare(*request.Source)
		if err != nil {
			return preparation, err
		}
		if request.Source.Type == "path" {
			preparation.localWidth, preparation.localHeight, err = preparation.source.LocalDimensions()
			if err != nil {
				return preparation, err
			}
		}
	}
	return preparation, nil
}

// Source returns the checked Source Image, or nil for text-to-image.
func (p *Preparation) Source() *sourceimage.Prepared { return p.source }

// Resolve applies family and profile settings and components, returning the
// requirements to check before upload or enqueue. Warnings also accompany a
// resolution failure so Direct Execution can retain them at its interface.
func (p *Preparation) Resolve(inventory []ModelIdentifier, random io.Reader) (capability.Entry, []result.Warning, error) {
	request, effective, profileGenerate := p.request, p.effective, p.profileGenerate
	main, err := ResolveFamilyMain(inventory, effective.Model)
	if err != nil {
		return capability.Entry{}, nil, err
	}
	if profileGenerate != nil {
		if err := validateProfileApplicability(request.Profile, *profileGenerate, main); err != nil {
			return capability.Entry{}, nil, err
		}
		effective = applyProfileSettings(effective, *profileGenerate)
	}
	var profileWarnings []result.Warning
	if profileGenerate != nil {
		effective, profileWarnings = applyProfileComponents(effective, *profileGenerate, main, inventory)
	}
	adapter, err := adapterForBase(main.Base)
	if err != nil {
		return capability.Entry{}, profileWarnings, err
	}
	effective = applyModeDefaults(effective)
	alignment := adapter.alignment()
	if effective.Source != nil {
		if request.Source.Type == "path" {
			if err := checkSourceSize(p.localWidth, p.localHeight, alignment); err != nil {
				return capability.Entry{}, profileWarnings, err
			}
			if effective.Width == nil {
				effective.Width, effective.Height = new(p.localWidth/alignment*alignment), new(p.localHeight/alignment*alignment)
			}
		}
	}
	resolved, err := adapter.resolve(effective, main, inventory, random)
	if err != nil {
		return capability.Entry{}, profileWarnings, err
	}
	entry, err := CapabilityEntry(resolved)
	if err != nil {
		return capability.Entry{}, profileWarnings, err
	}
	if effective.Source == nil {
		// Text-to-image keeps its existing invocation-only preflight.
		entry.Endpoints = nil
	}
	p.adapter, p.resolved = adapter, resolved
	return entry, profileWarnings, nil
}

// Compile checks the resolved Source Image and compiles the family's graph.
func (p *Preparation) Compile(source sourceimage.Resolved) (EnqueueRequest, int, error) {
	resolved := p.resolved
	if p.source != nil {
		alignment := p.adapter.alignment()
		if source.Uploaded && (source.Image.Width != p.localWidth || source.Image.Height != p.localHeight) {
			return EnqueueRequest{}, 0, &httpclient.InvalidResponseError{Err: fmt.Errorf("uploaded source dimensions %d × %d differ from local image dimensions %d × %d", source.Image.Width, source.Image.Height, p.localWidth, p.localHeight)}
		}
		if err := checkSourceSize(source.Image.Width, source.Image.Height, alignment); err != nil {
			return EnqueueRequest{}, 0, err
		}
		if p.request.Width == nil {
			resolved.Request.Width, resolved.Request.Height = new(source.Image.Width/alignment*alignment), new(source.Image.Height/alignment*alignment)
		}
		resolved.SourceImage = source.Image
	}
	enqueueRequest, err := Compile(resolved)
	if err != nil {
		return EnqueueRequest{}, 0, err
	}
	p.resolved = resolved
	return enqueueRequest, *resolved.Request.OutputCount, nil
}

// Receipt records the accepted generation using its complete resolved settings.
func (p *Preparation) Receipt(queueReceipt QueueReceipt, source sourceimage.Resolved, profileWarnings []result.Warning) ExecutionReceipt {
	request, resolved := p.request, p.resolved
	receipt := ExecutionReceipt{
		Family:           resolved.Models.Main.Base,
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
			ComponentKeys:  p.adapter.componentKeys(resolved),
			Seeds:          slices.Clone(resolved.Seeds),
			Strength:       resolved.Request.Strength,
		},
		Queue:    queueReceipt,
		Outputs:  []Output{},
		Warnings: append([]result.Warning{}, profileWarnings...),
	}
	if p.source != nil {
		receipt.SourceImage = &source.Image
		receipt.SourceUploaded = new(source.Uploaded)
	}
	return receipt
}

func checkSourceSize(width, height, alignment int) error {
	if width < alignment || height < alignment {
		return operation.InvalidRequest(fmt.Sprintf("source image width and height must each be at least %d pixels", alignment))
	}
	return nil
}
