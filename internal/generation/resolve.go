package generation

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
)

// Resolution contains the complete generation values and exact installed
// model identifiers needed to compile a family execution graph.
type Resolution struct {
	Request     Request
	Models      ResolvedModels
	Seeds       []uint32
	SourceImage images.Reference
	Loras       []ResolvedLoRA
}

type ResolvedLoRA struct {
	Model  ModelIdentifier
	Weight float64
}

var (
	animaVAERequirement     = graphops.ComponentRequirement{Kind: "vae", Base: "anima", ModelType: "vae"}
	sdxlVAERequirement      = graphops.ComponentRequirement{Kind: "vae", Base: "sdxl", ModelType: "vae"}
	qwen3EncoderRequirement = graphops.ComponentRequirement{Kind: "qwen3_encoder", Base: "any", ModelType: "qwen3_encoder"}
	fluxVAERequirement      = graphops.ComponentRequirement{Kind: "vae", Base: "flux", ModelType: "vae"}
	fluxT5Requirement       = graphops.ComponentRequirement{Kind: "t5_encoder", Base: "any", ModelType: "t5_encoder"}
	fluxCLIPRequirement     = graphops.ComponentRequirement{Kind: "clip_embed", Base: "any", ModelType: "clip_embed"}
)

func resolveSDXL(request Request, mainModel ModelIdentifier, inventory []ModelIdentifier, random io.Reader, alignment int, entry capability.Entry) (Resolution, error) {
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
	if err := validateGenerationDimensions(*resolved.Width, *resolved.Height, alignment); err != nil {
		return Resolution{}, err
	}
	if *resolved.Steps < 1 {
		return Resolution{}, operation.InvalidRequest("steps must be positive")
	}
	if !entry.SupportsScheduler(*resolved.Scheduler) {
		return Resolution{}, operation.InvalidField("scheduler", "scheduler is not supported for SDXL")
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
	loras, err := resolveLoRAs(request.Loras, inventory, mainModel.Base)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Request: resolved, Models: models, Seeds: seeds, Loras: loras}, nil
}

func resolveAnima(request Request, mainModel ModelIdentifier, inventory []ModelIdentifier, random io.Reader, alignment int, entry capability.Entry) (Resolution, error) {
	if request.Components != nil {
		if request.Components.T5Encoder != nil {
			return Resolution{}, operation.InvalidRequest("t5_encoder is not applicable to Anima")
		}
		if request.Components.CLIPEmbed != nil {
			return Resolution{}, operation.InvalidRequest("clip_embed is not applicable to Anima")
		}
	}
	resolved := applyAnimaDefaults(request)
	if err := validateAnimaSettings(resolved, alignment, entry); err != nil {
		return Resolution{}, err
	}
	seeds, err := assignSeeds(&resolved, random, "Anima")
	if err != nil {
		return Resolution{}, err
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
		return Resolution{}, fmt.Errorf("resolve Anima VAE: %w", err)
	}
	encoder, err := graphops.ResolveComponent(inventory, encoderSelector, qwen3EncoderRequirement)
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve Qwen3 encoder: %w", err)
	}
	return Resolution{
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
	if request.Model == "" && request.Profile == "" {
		return operation.InvalidRequest("model is required")
	}
	if request.PositivePrompt == "" {
		return operation.InvalidRequest("positive prompt is required")
	}
	if (request.Width == nil) != (request.Height == nil) {
		field := "height"
		if request.Width == nil {
			field = "width"
		}
		return operation.InvalidField(field, "width and height must be supplied together or both omitted")
	}
	if request.Source != nil {
		if err := request.Source.Validate(); err != nil {
			return err
		}
	}
	if request.Strength != nil {
		if request.Source == nil || math.IsNaN(*request.Strength) || math.IsInf(*request.Strength, 0) || *request.Strength <= 0 || *request.Strength > 1 {
			return operation.InvalidField("strength", "strength requires a source and must be finite, greater than 0, and at most 1")
		}
	}
	if request.Loras != nil && len(request.Loras) == 0 {
		return operation.InvalidField("loras", "loras must be a non-empty list")
	}
	for index, lora := range request.Loras {
		if lora.Model == "" {
			return operation.InvalidField(fmt.Sprintf("loras.%d.model", index), "LoRA model selector is required")
		}
		if lora.Weight != nil && (math.IsNaN(*lora.Weight) || math.IsInf(*lora.Weight, 0) || *lora.Weight < -10 || *lora.Weight > 10) {
			return operation.InvalidField(fmt.Sprintf("loras.%d.weight", index), "LoRA weight must be finite and between -10 and 10")
		}
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

func validateAnimaSettings(request Request, alignment int, entry capability.Entry) error {
	if err := validateGenerationDimensions(*request.Width, *request.Height, alignment); err != nil {
		return err
	}
	if *request.Steps < 1 {
		return operation.InvalidRequest("steps must be positive")
	}
	if !entry.SupportsScheduler(*request.Scheduler) {
		return operation.InvalidField("scheduler", "scheduler is not supported for Anima")
	}
	if math.IsNaN(*request.Guidance) || math.IsInf(*request.Guidance, 0) || *request.Guidance < 1 {
		return operation.InvalidRequest("guidance must be finite and at least 1")
	}
	if *request.OutputCount < 1 {
		return operation.InvalidRequest("output count must be positive")
	}
	return nil
}
