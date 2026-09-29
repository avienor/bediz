package upscale

import (
	"fmt"
	"io"
	"math"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profileexecution"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
)

// Preparation holds upscale-specific resolution and receipt state. Direct
// Execution owns the network lifecycle and closes the prepared Source Image.
type Preparation struct {
	request        Request
	effective      Request
	profileUpscale *profiles.Upscale
	source         *sourceimage.Prepared
	resolved       Resolution
	outputWidth    int
	outputHeight   int
}

// Prepare validates the request and profile before any InvokeAI request.
func Prepare(request Request) (*Preparation, error) {
	if err := validateRequest(request, false); err != nil {
		return nil, err
	}
	effective := request
	var profileUpscale *profiles.Upscale
	if request.Profile != "" {
		profile, err := profileexecution.Load(request.Profile, profileLoadError)
		if err != nil {
			return nil, err
		}
		if profile.Upscale == nil {
			return nil, operation.InvalidRequest("profile has no upscale section")
		}
		profileUpscale = profile.Upscale
		if effective.Model == "" && profileUpscale.Model != nil {
			effective.Model = *profileUpscale.Model
		}
		effective = applyProfileSettings(effective, *profileUpscale)
	}
	if err := ValidateRequest(effective); err != nil {
		return nil, err
	}
	source, err := sourceimage.Prepare(request.Source)
	if err != nil {
		return nil, err
	}
	return &Preparation{request: request, effective: effective, profileUpscale: profileUpscale, source: source}, nil
}

func (p *Preparation) Source() *sourceimage.Prepared { return p.source }

// Resolve returns the tested invocation requirements and skipped profile
// preferences, including when component resolution fails.
func (p *Preparation) Resolve(inventory []graphops.ModelIdentifier, random io.Reader) (capability.Entry, []result.Warning, error) {
	effective := p.effective
	var warnings []result.Warning
	if p.profileUpscale != nil {
		main, err := ResolveMain(inventory, effective.Model)
		if err != nil {
			return capability.Entry{}, nil, err
		}
		effective, warnings = applyProfileComponents(effective, *p.profileUpscale, main, inventory)
	}
	resolved, err := Resolve(effective, inventory, random)
	if err != nil {
		return capability.Entry{}, warnings, err
	}
	family, _ := familyFor(resolved.Models.Main.Base)
	p.resolved = resolved
	entry := family.entry()
	return entry, warnings, nil
}

// Compile validates the inspected Source Image and compiles the upscale graph.
func (p *Preparation) Compile(source sourceimage.Resolved) (graphops.EnqueueRequest, int, error) {
	image := source.Image
	scale := *p.resolved.Request.Scale
	if image.Width < 1 || image.Height < 1 || image.Width > math.MaxInt/scale || image.Height > math.MaxInt/scale {
		return graphops.EnqueueRequest{}, 0, &httpclient.InvalidResponseError{Err: fmt.Errorf("source image has invalid dimensions %d × %d", image.Width, image.Height)}
	}
	width, height := image.Width*scale/8*8, image.Height*scale/8*8
	graph, err := Compile(p.resolved, image)
	if err != nil {
		return graphops.EnqueueRequest{}, 0, err
	}
	p.outputWidth, p.outputHeight = width, height
	return graph, 1, nil
}

// Receipt records the accepted upscale with the submitted request and exact
// resolved settings.
func (p *Preparation) Receipt(queue QueueReceipt, source sourceimage.Resolved, warnings []result.Warning) ExecutionReceipt {
	resolved := p.resolved
	componentKeys := map[string]string{
		"upscale_model":   resolved.Models.UpscaleModel.Key,
		"tile_controlnet": resolved.Models.TileControlNet.Key,
	}
	if resolved.Models.VAE.Key != "" {
		componentKeys["vae"] = resolved.Models.VAE.Key
	}
	return ExecutionReceipt{
		SubmittedRequest: p.request, SourceImage: source.Image, SourceUploaded: source.Uploaded,
		ResolvedSettings: ResolvedSettings{
			Profile: p.request.Profile, PositivePrompt: resolved.Request.PositivePrompt, NegativePrompt: resolved.Request.NegativePrompt,
			Scale: *resolved.Request.Scale, Creativity: *resolved.Request.Creativity, Structure: *resolved.Request.Structure,
			Steps: *resolved.Request.Steps, Scheduler: *resolved.Request.Scheduler, Guidance: *resolved.Request.Guidance,
			TileSize: *resolved.Request.TileSize, TileOverlap: *resolved.Request.TileOverlap,
			OutputWidth: p.outputWidth, OutputHeight: p.outputHeight, BoardID: resolved.Request.BoardID,
			ModelKey: resolved.Models.Main.Key, ComponentKeys: componentKeys, Seeds: []uint32{resolved.Seed},
		},
		Queue: queue, Outputs: []Output{}, Warnings: append([]result.Warning{}, warnings...),
	}
}
