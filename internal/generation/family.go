package generation

import (
	"fmt"
	"io"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/operation"
)

// A family adapter owns the behavior that varies with the selected main model.
// Register an adapter here when its graph and capability entry have been tested.
type familyAdapter interface {
	resolve(Request, ModelIdentifier, []ModelIdentifier, io.Reader) (Resolution, error)
	compile(Resolution) (EnqueueRequest, error)
	invocations() []capability.InvocationRequirement
	componentKeys(Resolution) map[string]string
	validateRecall(ModelIdentifier, *int, *int, *int) error
	synchronization(ResolvedSettings) (SyncSettings, []string)
}

var families = map[string]familyAdapter{
	"anima": animaAdapter{},
	"sdxl":  sdxlAdapter{},
	"flux":  fluxAdapter{},
}

type animaAdapter struct{}
type sdxlAdapter struct{}
type fluxAdapter struct{}

func (fluxAdapter) resolve(request Request, main ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	return resolveFLUX(request, main, inventory, random)
}

func (fluxAdapter) compile(resolved Resolution) (EnqueueRequest, error) { return CompileFLUX(resolved) }
func (fluxAdapter) invocations() []capability.InvocationRequirement {
	return capability.FLUXGenerationEntry().Invocations
}
func (fluxAdapter) componentKeys(resolved Resolution) map[string]string {
	return map[string]string{"vae": resolved.Models.VAE.Key, "t5_encoder": resolved.Models.T5Encoder.Key, "clip_embed": resolved.Models.CLIPEmbed.Key}
}
func (fluxAdapter) validateRecall(model ModelIdentifier, width, height, steps *int) error {
	if err := validateFLUXMain(model); err != nil {
		return err
	}
	return validateAlignedRecall(width, height, steps, 16)
}
func (fluxAdapter) synchronization(settings ResolvedSettings) (SyncSettings, []string) {
	fields := []string{"scheduler"}
	if settings.Guidance != nil {
		fields = append(fields, "guidance")
	}
	fields = append(fields, "vae", "t5_encoder", "clip_embed", "output_count", "board_id")
	return SyncSettings{Model: settings.ModelKey, PositivePrompt: settings.PositivePrompt, NegativePrompt: settings.NegativePrompt, Width: settings.Width, Height: settings.Height, Steps: settings.Steps, Seed: settings.Seeds[0]}, fields
}

func (sdxlAdapter) resolve(request Request, main ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	return resolveSDXL(request, main, inventory, random)
}

func (sdxlAdapter) compile(resolved Resolution) (EnqueueRequest, error) {
	return CompileSDXL(resolved)
}

func (sdxlAdapter) invocations() []capability.InvocationRequirement {
	return capability.SDXLGenerationEntry().Invocations
}

func (sdxlAdapter) componentKeys(resolved Resolution) map[string]string {
	keys := map[string]string{}
	if resolved.Models.VAE.Key != "" {
		keys["vae"] = resolved.Models.VAE.Key
	}
	return keys
}

func (sdxlAdapter) validateRecall(_ ModelIdentifier, width, height, steps *int) error {
	return validateAlignedRecall(width, height, steps, 8)
}

func (sdxlAdapter) synchronization(settings ResolvedSettings) (SyncSettings, []string) {
	return SyncSettings{
		Model: settings.ModelKey, PositivePrompt: settings.PositivePrompt, NegativePrompt: settings.NegativePrompt,
		Width: settings.Width, Height: settings.Height, Steps: settings.Steps, Seed: settings.Seeds[0],
		Additional: []capability.RecallPatchField{{Requirement: capability.SDXLCFGRecallField, Value: settings.Guidance}},
	}, []string{"scheduler", "vae", "output_count", "board_id"}
}

func (animaAdapter) resolve(request Request, main ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	return resolveAnima(request, main, inventory, random)
}

func (animaAdapter) compile(resolved Resolution) (EnqueueRequest, error) {
	return CompileAnima(resolved)
}

func (animaAdapter) invocations() []capability.InvocationRequirement {
	return capability.AnimaGenerationEntry().Invocations
}

func (animaAdapter) componentKeys(resolved Resolution) map[string]string {
	return map[string]string{"vae": resolved.Models.VAE.Key, "qwen3_encoder": resolved.Models.Qwen3Encoder.Key}
}

func (animaAdapter) validateRecall(_ ModelIdentifier, width, height, steps *int) error {
	return validateAlignedRecall(width, height, steps, 8)
}

func validateAlignedRecall(width, height, steps *int, alignment int) error {
	if width != nil && (*width < 64 || *width%alignment != 0 || *height < 64 || *height%alignment != 0) {
		return operation.InvalidRequest(fmt.Sprintf("width and height must be multiples of %d and at least 64 for Recall", alignment))
	}
	if steps != nil && *steps < 1 {
		return operation.InvalidRequest("steps must be positive")
	}
	return nil
}

// SyncSettings describes the tested fields sent to InvokeAI Recall.
type SyncSettings struct {
	Model          string
	PositivePrompt string
	NegativePrompt string
	Width          int
	Height         int
	Steps          int
	Seed           uint32
	Additional     []capability.RecallPatchField
}

func (animaAdapter) synchronization(settings ResolvedSettings) (SyncSettings, []string) {
	return SyncSettings{
		Model: settings.ModelKey, PositivePrompt: settings.PositivePrompt, NegativePrompt: settings.NegativePrompt,
		Width: settings.Width, Height: settings.Height, Steps: settings.Steps, Seed: settings.Seeds[0],
	}, []string{"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id"}
}

func adapterForBase(base string) (familyAdapter, error) {
	adapter, ok := families[base]
	if !ok {
		return nil, operation.UnsupportedCapability(fmt.Sprintf("model family %q is not supported for generation", base))
	}
	return adapter, nil
}

// ResolveFamilyMain selects an installed main model across all bases, then
// checks whether its family has a registered adapter.
func ResolveFamilyMain(inventory []ModelIdentifier, selector string) (ModelIdentifier, error) {
	for _, model := range inventory {
		if model.Key == selector {
			if model.Type != "main" {
				return ModelIdentifier{}, operation.UnsupportedCapability(fmt.Sprintf("model %q has type %q; expected type %q", model.Key, model.Type, "main"))
			}
			if _, err := adapterForBase(model.Base); err != nil {
				return ModelIdentifier{}, err
			}
			return completeModelIdentifier(model)
		}
	}
	var matches []ModelIdentifier
	for _, model := range inventory {
		if model.Type == "main" && model.Name == selector {
			matches = append(matches, model)
		}
	}
	if len(matches) > 1 {
		candidates, err := selectionCandidates(matches)
		if err != nil {
			return ModelIdentifier{}, err
		}
		return ModelIdentifier{}, operation.SelectionRequired("main_model", selector, candidates)
	}
	if len(matches) == 1 {
		if _, err := adapterForBase(matches[0].Base); err != nil {
			return ModelIdentifier{}, err
		}
		return completeModelIdentifier(matches[0])
	}
	return ModelIdentifier{}, operation.InvalidRequest(fmt.Sprintf("model selector %q did not resolve to an installed model", selector))
}

// ValidateRecall checks settings whose meaning depends on the selected family.
func ValidateRecall(model ModelIdentifier, width, height, steps *int) error {
	adapter, err := adapterForBase(model.Base)
	if err != nil {
		return err
	}
	return adapter.validateRecall(model, width, height, steps)
}

// SynchronizationFields returns the family-specific Recall patch and controls
// that stock InvokeAI cannot restore after direct execution.
func SynchronizationFields(family string, settings ResolvedSettings) (SyncSettings, []string, error) {
	adapter, err := adapterForBase(family)
	if err != nil {
		return SyncSettings{}, nil, err
	}
	patch, notRestored := adapter.synchronization(settings)
	return patch, notRestored, nil
}
