package generation

import (
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
)

// ResolveFLUX resolves a supported FLUX.1 main model and its three components.
func ResolveFLUX(request Request, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	if err := validateCommonRequest(request); err != nil {
		return Resolution{}, err
	}
	main, err := ResolveFamilyMain(inventory, request.Model)
	if err != nil {
		return Resolution{}, err
	}
	if main.Base != "flux" {
		return Resolution{}, operation.UnsupportedCapability(fmt.Sprintf("model %q is not a FLUX.1 main model", main.Key))
	}
	return resolveFLUX(request, main, inventory, random)
}

func validateFLUXMain(main ModelIdentifier) error {
	if !capability.SupportsFLUXMain(main.Variant, main.Format) {
		return operation.UnsupportedCapability(fmt.Sprintf("FLUX.1 model %q has unsupported variant %q or format %q", main.Key, main.Variant, main.Format))
	}
	return nil
}

func resolveFLUX(request Request, main ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	if err := validateFLUXMain(main); err != nil {
		return Resolution{}, err
	}
	if request.Components != nil && request.Components.Qwen3Encoder != nil {
		return Resolution{}, operation.InvalidRequest("qwen3_encoder is not applicable to FLUX.1")
	}
	if request.NegativePrompt != "" {
		return Resolution{}, operation.InvalidRequest("negative_prompt is not applicable to FLUX.1")
	}
	resolved := request
	if resolved.Width == nil {
		resolved.Width, resolved.Height = new(1024), new(1024)
	}
	if resolved.Steps == nil {
		if main.Variant == "dev" {
			resolved.Steps = new(30)
		} else {
			resolved.Steps = new(4)
		}
	}
	if resolved.Scheduler == nil {
		resolved.Scheduler = new("euler")
	}
	if main.Variant == "dev" {
		if resolved.Guidance == nil {
			resolved.Guidance = new(4.0)
		}
		if math.IsNaN(*resolved.Guidance) || math.IsInf(*resolved.Guidance, 0) || *resolved.Guidance < 1 {
			return Resolution{}, operation.InvalidRequest("guidance must be finite and at least 1")
		}
	} else if resolved.Guidance != nil {
		return Resolution{}, operation.InvalidRequest("guidance is not applicable to FLUX.1 schnell")
	}
	if resolved.OutputCount == nil {
		resolved.OutputCount = new(1)
	}
	if *resolved.Width < 1 || *resolved.Width%16 != 0 || *resolved.Height < 1 || *resolved.Height%16 != 0 {
		return Resolution{}, operation.InvalidRequest("width and height must be positive multiples of 16")
	}
	if *resolved.Steps < 1 {
		return Resolution{}, operation.InvalidRequest("steps must be positive")
	}
	if !slices.Contains([]string{"euler", "heun", "lcm"}, *resolved.Scheduler) {
		return Resolution{}, operation.InvalidRequest("scheduler is not supported for FLUX.1")
	}
	if *resolved.OutputCount < 1 {
		return Resolution{}, operation.InvalidRequest("output count must be positive")
	}
	seeds, err := assignSeeds(&resolved, random, "FLUX.1")
	if err != nil {
		return Resolution{}, err
	}
	var vaeSelector, t5Selector, clipSelector string
	if request.Components != nil {
		if request.Components.VAE != nil {
			vaeSelector = *request.Components.VAE
			if vaeSelector == "" {
				return Resolution{}, operation.InvalidRequest("VAE override must be a model key or unique name")
			}
		}
		if request.Components.T5Encoder != nil {
			t5Selector = *request.Components.T5Encoder
			if t5Selector == "" {
				return Resolution{}, operation.InvalidRequest("T5 encoder override must be a model key or unique name")
			}
		}
		if request.Components.CLIPEmbed != nil {
			clipSelector = *request.Components.CLIPEmbed
			if clipSelector == "" {
				return Resolution{}, operation.InvalidRequest("CLIP Embed override must be a model key or unique name")
			}
		}
	}
	vae, err := graphops.ResolveComponent(inventory, vaeSelector, fluxVAERequirement)
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve FLUX.1 VAE: %w", err)
	}
	t5, err := graphops.ResolveComponent(inventory, t5Selector, fluxT5Requirement)
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve FLUX.1 T5 encoder: %w", err)
	}
	clip, err := graphops.ResolveComponent(inventory, clipSelector, fluxCLIPRequirement)
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve FLUX.1 CLIP Embed: %w", err)
	}
	return Resolution{Request: resolved, Models: ResolvedModels{Main: main, VAE: vae, T5Encoder: t5, CLIPEmbed: clip}, Seeds: seeds}, nil
}
