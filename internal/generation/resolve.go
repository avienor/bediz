package generation

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
)

// Resolution contains the complete generation values and exact installed
// model identifiers needed to compile a family execution graph.
type Resolution struct {
	Request Request
	Models  ResolvedModels
	Seeds   []uint32
}

// AnimaResolution is the Anima compiler's resolution value.
type AnimaResolution = Resolution

var (
	animaVAERequirement     = graphops.ComponentRequirement{Kind: "vae", Base: "anima", ModelType: "vae"}
	sdxlVAERequirement      = graphops.ComponentRequirement{Kind: "vae", Base: "sdxl", ModelType: "vae"}
	qwen3EncoderRequirement = graphops.ComponentRequirement{Kind: "qwen3_encoder", Base: "any", ModelType: "qwen3_encoder"}
	fluxVAERequirement      = graphops.ComponentRequirement{Kind: "vae", Base: "flux", ModelType: "vae"}
	fluxT5Requirement       = graphops.ComponentRequirement{Kind: "t5_encoder", Base: "any", ModelType: "t5_encoder"}
	fluxCLIPRequirement     = graphops.ComponentRequirement{Kind: "clip_embed", Base: "any", ModelType: "clip_embed"}
)

// ResolveSDXL resolves an SDXL main model and its optional explicit VAE override.
func ResolveSDXL(request Request, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	if err := validateCommonRequest(request); err != nil {
		return Resolution{}, err
	}
	mainModel, err := ResolveFamilyMain(inventory, request.Model)
	if err != nil {
		return Resolution{}, err
	}
	if mainModel.Base != "sdxl" {
		return Resolution{}, operation.UnsupportedCapability(fmt.Sprintf("model %q is not an SDXL main model", mainModel.Key))
	}
	return resolveSDXL(request, mainModel, inventory, random)
}

func resolveSDXL(request Request, mainModel ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	if request.Components != nil {
		if request.Components.Qwen3Encoder != nil {
			return Resolution{}, operation.InvalidRequest("qwen3_encoder is not applicable to SDXL")
		}
		if request.Components.T5Encoder != nil {
			return Resolution{}, operation.InvalidRequest("t5_encoder is not applicable to SDXL")
		}
		if request.Components.CLIPEmbed != nil {
			return Resolution{}, operation.InvalidRequest("clip_embed is not applicable to SDXL")
		}
	}
	resolved := request
	if resolved.Width == nil {
		resolved.Width, resolved.Height = new(1024), new(1024)
	}
	if resolved.Steps == nil {
		resolved.Steps = new(30)
	}
	if resolved.Scheduler == nil {
		resolved.Scheduler = new("dpmpp_3m_k")
	}
	if resolved.Guidance == nil {
		resolved.Guidance = new(7.0)
	}
	if resolved.OutputCount == nil {
		resolved.OutputCount = new(1)
	}
	if *resolved.Width < 1 || *resolved.Width%8 != 0 || *resolved.Height < 1 || *resolved.Height%8 != 0 {
		return Resolution{}, operation.InvalidRequest("width and height must be positive multiples of 8")
	}
	if *resolved.Steps < 1 {
		return Resolution{}, operation.InvalidRequest("steps must be positive")
	}
	if !slices.Contains(sdxlSchedulers, *resolved.Scheduler) {
		return Resolution{}, operation.InvalidRequest("scheduler is not supported for SDXL")
	}
	if math.IsNaN(*resolved.Guidance) || math.IsInf(*resolved.Guidance, 0) || *resolved.Guidance < 1 {
		return Resolution{}, operation.InvalidRequest("guidance must be finite and at least 1")
	}
	if *resolved.OutputCount < 1 {
		return Resolution{}, operation.InvalidRequest("output count must be positive")
	}
	seeds, err := assignSeeds(&resolved, random, "SDXL")
	if err != nil {
		return Resolution{}, err
	}
	models := ResolvedModels{Main: mainModel}
	if request.Components != nil && request.Components.VAE != nil {
		if *request.Components.VAE == "" {
			return Resolution{}, operation.InvalidRequest("VAE override must be a model key or unique name")
		}
		vae, err := graphops.ResolveUniqueCompatible(inventory, *request.Components.VAE, sdxlVAERequirement)
		if err != nil {
			return Resolution{}, fmt.Errorf("resolve SDXL VAE: %w", err)
		}
		models.VAE = vae
	}
	return Resolution{Request: resolved, Models: models, Seeds: seeds}, nil
}

var sdxlSchedulers = []string{
	"ddim", "ddpm", "deis", "deis_k", "lms", "lms_k", "pndm", "heun", "heun_k", "euler", "euler_k", "euler_a",
	"kdpm_2", "kdpm_2_k", "kdpm_2_a", "kdpm_2_a_k", "dpmpp_2s", "dpmpp_2s_k", "dpmpp_2m", "dpmpp_2m_k",
	"dpmpp_2m_sde", "dpmpp_2m_sde_k", "dpmpp_3m", "dpmpp_3m_k", "dpmpp_sde", "dpmpp_sde_k", "er_sde",
	"unipc", "unipc_k", "lcm", "tcd",
}

// ResolveAnima applies Anima family defaults and resolves the required
// installed models without consulting browser state.
func ResolveAnima(request Request, inventory []ModelIdentifier, random io.Reader) (AnimaResolution, error) {
	if err := validateCommonRequest(request); err != nil {
		return AnimaResolution{}, err
	}
	mainModel, err := ResolveFamilyMain(inventory, request.Model)
	if err != nil {
		return AnimaResolution{}, fmt.Errorf("resolve Anima main model: %w", err)
	}
	if mainModel.Base != "anima" {
		return AnimaResolution{}, operation.UnsupportedCapability(fmt.Sprintf("model %q is not an Anima main model", mainModel.Key))
	}
	return resolveAnima(request, mainModel, inventory, random)
}

func resolveAnima(request Request, mainModel ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (AnimaResolution, error) {
	if request.Components != nil {
		if request.Components.T5Encoder != nil {
			return AnimaResolution{}, operation.InvalidRequest("t5_encoder is not applicable to Anima")
		}
		if request.Components.CLIPEmbed != nil {
			return AnimaResolution{}, operation.InvalidRequest("clip_embed is not applicable to Anima")
		}
	}
	resolved := applyAnimaDefaults(request)
	if err := validateAnimaSettings(resolved); err != nil {
		return AnimaResolution{}, err
	}
	seeds, err := assignSeeds(&resolved, random, "Anima")
	if err != nil {
		return AnimaResolution{}, err
	}

	var vaeSelector string
	var encoderSelector string
	if request.Components != nil {
		if request.Components.VAE != nil {
			vaeSelector = *request.Components.VAE
		}
		if request.Components.Qwen3Encoder != nil {
			encoderSelector = *request.Components.Qwen3Encoder
		}
	}
	vae, err := graphops.ResolveComponent(inventory, vaeSelector, animaVAERequirement)
	if err != nil {
		return AnimaResolution{}, fmt.Errorf("resolve Anima VAE: %w", err)
	}
	encoder, err := graphops.ResolveComponent(inventory, encoderSelector, qwen3EncoderRequirement)
	if err != nil {
		return AnimaResolution{}, fmt.Errorf("resolve Qwen3 encoder: %w", err)
	}
	return AnimaResolution{
		Request: resolved,
		Models:  ResolvedModels{Main: mainModel, VAE: vae, Qwen3Encoder: encoder},
		Seeds:   seeds,
	}, nil
}

func assignSeeds(request *Request, random io.Reader, family string) ([]uint32, error) {
	seeds := make([]uint32, *request.OutputCount)
	if request.Seed == nil {
		for index := range seeds {
			var encoded [4]byte
			if _, err := io.ReadFull(random, encoded[:]); err != nil {
				return nil, fmt.Errorf("assign random %s seed %d: %w", family, index+1, err)
			}
			seeds[index] = binary.LittleEndian.Uint32(encoded[:])
		}
		request.Seed = new(seeds[0])
	} else {
		seed := *request.Seed
		for index := range seeds {
			seeds[index] = seed
			seed++
		}
	}
	return seeds, nil
}

func validateCommonRequest(request Request) error {
	if request.SchemaVersion != 1 {
		return operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.Model == "" {
		return operation.InvalidRequest("model is required")
	}
	if request.PositivePrompt == "" {
		return operation.InvalidRequest("positive prompt is required")
	}
	if (request.Width == nil) != (request.Height == nil) {
		return operation.InvalidRequest("width and height must be supplied together or both omitted")
	}
	return nil
}

func applyAnimaDefaults(request Request) Request {
	resolved := request
	if resolved.Width == nil && resolved.Height == nil {
		resolved.Width = new(1024)
		resolved.Height = new(1024)
	}
	if resolved.Steps == nil {
		resolved.Steps = new(30)
	}
	if resolved.Scheduler == nil {
		resolved.Scheduler = new("euler")
	}
	if resolved.Guidance == nil {
		resolved.Guidance = new(4.5)
	}
	if resolved.OutputCount == nil {
		resolved.OutputCount = new(1)
	}
	return resolved
}

func validateAnimaSettings(request Request) error {
	if *request.Width < 1 || *request.Width%8 != 0 || *request.Height < 1 || *request.Height%8 != 0 {
		return operation.InvalidRequest("width and height must be positive multiples of 8")
	}
	if *request.Steps < 1 {
		return operation.InvalidRequest("steps must be positive")
	}
	if !slices.Contains([]string{"euler", "heun", "dpmpp_2m", "dpmpp_2m_sde", "er_sde", "lcm"}, *request.Scheduler) {
		return operation.InvalidRequest("scheduler is not supported for Anima")
	}
	if math.IsNaN(*request.Guidance) || math.IsInf(*request.Guidance, 0) || *request.Guidance < 1 {
		return operation.InvalidRequest("guidance must be finite and at least 1")
	}
	if *request.OutputCount < 1 {
		return operation.InvalidRequest("output count must be positive")
	}
	return nil
}
