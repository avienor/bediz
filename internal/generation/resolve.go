package generation

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/avienor/bediz/internal/operation"
)

// AnimaResolution contains the complete generation values and exact installed
// model identifiers needed to compile one Anima execution graph.
type AnimaResolution struct {
	Request Request
	Models  ResolvedModels
	Seeds   []uint32
}

type modelRequirement struct {
	kind      string
	base      string
	modelType string
}

var (
	animaMainRequirement    = modelRequirement{kind: "main_model", base: "anima", modelType: "main"}
	animaVAERequirement     = modelRequirement{kind: "vae", base: "anima", modelType: "vae"}
	qwen3EncoderRequirement = modelRequirement{kind: "qwen3_encoder", base: "any", modelType: "qwen3_encoder"}
)

// ResolveAnima applies Anima family defaults and resolves the required
// installed models without consulting browser state.
func ResolveAnima(request Request, inventory []ModelIdentifier, random io.Reader) (AnimaResolution, error) {
	if err := validateAnimaRequest(request); err != nil {
		return AnimaResolution{}, err
	}

	resolved := applyAnimaDefaults(request)
	seeds := make([]uint32, *resolved.OutputCount)
	if resolved.Seed == nil {
		for index := range seeds {
			var encoded [4]byte
			if _, err := io.ReadFull(random, encoded[:]); err != nil {
				return AnimaResolution{}, fmt.Errorf("assign random Anima seed %d: %w", index+1, err)
			}
			seeds[index] = binary.LittleEndian.Uint32(encoded[:])
		}
		resolved.Seed = new(seeds[0])
	} else {
		seed := *resolved.Seed
		for index := range seeds {
			seeds[index] = seed
			seed++
		}
	}

	mainModel, err := resolveUniqueCompatible(inventory, request.Model, animaMainRequirement)
	if err != nil {
		return AnimaResolution{}, fmt.Errorf("resolve Anima main model: %w", err)
	}
	var vaeSelector string
	var encoderSelector string
	if request.Components != nil {
		vaeSelector = request.Components.VAE
		encoderSelector = request.Components.Qwen3Encoder
	}
	vae, err := resolveComponent(inventory, vaeSelector, animaVAERequirement)
	if err != nil {
		return AnimaResolution{}, fmt.Errorf("resolve Anima VAE: %w", err)
	}
	encoder, err := resolveComponent(inventory, encoderSelector, qwen3EncoderRequirement)
	if err != nil {
		return AnimaResolution{}, fmt.Errorf("resolve Qwen3 encoder: %w", err)
	}
	return AnimaResolution{
		Request: resolved,
		Models:  ResolvedModels{Main: mainModel, VAE: vae, Qwen3Encoder: encoder},
		Seeds:   seeds,
	}, nil
}

func validateAnimaRequest(request Request) error {
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
	return validateAnimaSettings(applyAnimaDefaults(request))
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

func resolveComponent(inventory []ModelIdentifier, selector string, requirement modelRequirement) (ModelIdentifier, error) {
	if selector != "" {
		return resolveUniqueCompatible(inventory, selector, requirement)
	}
	return resolveOnlyCompatible(inventory, requirement)
}

func resolveUniqueCompatible(inventory []ModelIdentifier, selector string, requirement modelRequirement) (ModelIdentifier, error) {
	for _, model := range inventory {
		if model.Key != selector {
			continue
		}
		if err := validateModelCompatibility(model, requirement); err != nil {
			return ModelIdentifier{}, err
		}
		return completeModelIdentifier(model)
	}
	var matches []ModelIdentifier
	for _, model := range inventory {
		if model.Name == selector {
			matches = append(matches, model)
		}
	}
	if len(matches) == 1 {
		if err := validateModelCompatibility(matches[0], requirement); err != nil {
			return ModelIdentifier{}, err
		}
		return completeModelIdentifier(matches[0])
	}
	if len(matches) > 1 {
		candidates, err := selectionCandidates(matches)
		if err != nil {
			return ModelIdentifier{}, err
		}
		return ModelIdentifier{}, operation.SelectionRequired(requirement.kind, selector, candidates)
	}
	return ModelIdentifier{}, operation.InvalidRequest(fmt.Sprintf("model selector %q did not resolve to an installed model", selector))
}

// ResolveAnimaMain resolves an explicit selector using the same rules as an
// Anima Generation Request, without requiring generation components.
func ResolveAnimaMain(inventory []ModelIdentifier, selector string) (ModelIdentifier, error) {
	return resolveUniqueCompatible(inventory, selector, animaMainRequirement)
}

func resolveOnlyCompatible(inventory []ModelIdentifier, requirement modelRequirement) (ModelIdentifier, error) {
	var matches []ModelIdentifier
	for _, model := range inventory {
		if model.Base == requirement.base && model.Type == requirement.modelType {
			matches = append(matches, model)
		}
	}
	if len(matches) == 1 {
		return completeModelIdentifier(matches[0])
	}
	if len(matches) > 1 {
		candidates, err := selectionCandidates(matches)
		if err != nil {
			return ModelIdentifier{}, err
		}
		return ModelIdentifier{}, operation.SelectionRequired(requirement.kind, "", candidates)
	}
	guidance := fmt.Sprintf(
		"install an InvokeAI model with base %q and type %q", requirement.base, requirement.modelType,
	)
	return ModelIdentifier{}, operation.MissingComponent(
		requirement.kind, requirement.base, requirement.modelType, guidance,
	)
}

func validateModelCompatibility(model ModelIdentifier, requirement modelRequirement) error {
	if model.Base == requirement.base && model.Type == requirement.modelType {
		return nil
	}
	return operation.UnsupportedCapability(fmt.Sprintf(
		"model %q has base %q and type %q; expected base %q and type %q",
		model.Key, model.Base, model.Type, requirement.base, requirement.modelType,
	))
}

func completeModelIdentifier(model ModelIdentifier) (ModelIdentifier, error) {
	if model.Key == "" || model.Hash == "" || model.Name == "" || model.Base == "" || model.Type == "" {
		return ModelIdentifier{}, operation.UnsupportedCapability(fmt.Sprintf(
			"installed model %q does not provide a complete InvokeAI model identifier", model.Key,
		))
	}
	return model, nil
}

func selectionCandidates(models []ModelIdentifier) ([]operation.SelectionCandidate, error) {
	for _, model := range models {
		if _, err := completeModelIdentifier(model); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(models, func(a, b ModelIdentifier) int { return cmp.Compare(a.Key, b.Key) })
	candidates := make([]operation.SelectionCandidate, 0, len(models))
	for _, model := range models {
		candidates = append(candidates, operation.SelectionCandidate{
			Key: model.Key, Name: model.Name, Base: model.Base, Type: model.Type,
		})
	}
	return candidates, nil
}
