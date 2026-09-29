package generation

import (
	"fmt"
	"io"
	"slices"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profiles"
)

// A family adapter owns the behavior that varies with the selected main model.
// Register an adapter here when its graph and capability entry have been tested.
type familyAdapter interface {
	resolve(Request, ModelIdentifier, []ModelIdentifier, io.Reader) (Resolution, error)
	compile(Resolution) (EnqueueRequest, error)
	capabilityEntry(Request) capability.Entry
	alignment() int
	profileApplicability(ModelIdentifier) profileApplicability
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

type profileApplicability struct {
	components []string
	guidance   bool
}

func (fluxAdapter) profileApplicability(main ModelIdentifier) profileApplicability {
	return profileApplicability{components: []string{"vae", "t5_encoder", "clip_embed"}, guidance: main.Variant != "schnell"}
}

func (sdxlAdapter) profileApplicability(ModelIdentifier) profileApplicability {
	return profileApplicability{components: []string{"vae"}, guidance: true}
}

func (animaAdapter) profileApplicability(ModelIdentifier) profileApplicability {
	return profileApplicability{components: []string{"vae", "qwen3_encoder"}, guidance: true}
}

func (adapter fluxAdapter) resolve(request Request, main ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	return resolveFLUX(request, main, inventory, random, adapter.alignment(), adapter.capabilityEntry(request))
}

func (fluxAdapter) alignment() int { return 16 }

func (fluxAdapter) compile(resolved Resolution) (EnqueueRequest, error) { return compileFLUX(resolved) }
func (fluxAdapter) capabilityEntry(request Request) capability.Entry {
	var entry capability.Entry
	if request.Source != nil {
		entry = capability.FLUXImageToImageEntry()
	} else {
		entry = capability.FLUXGenerationEntry()
	}
	if len(request.Loras) > 0 {
		return capability.WithFLUXLoRA(entry)
	}
	return entry
}
func (fluxAdapter) componentKeys(resolved Resolution) map[string]string {
	return map[string]string{"vae": resolved.Models.VAE.Key, "t5_encoder": resolved.Models.T5Encoder.Key, "clip_embed": resolved.Models.CLIPEmbed.Key}
}
func (adapter fluxAdapter) validateRecall(model ModelIdentifier, width, height, steps *int) error {
	if err := validateFLUXMain(model); err != nil {
		return err
	}
	return validateAlignedRecall(width, height, steps, adapter.alignment())
}
func (fluxAdapter) synchronization(settings ResolvedSettings) (SyncSettings, []string) {
	fields := []string{"scheduler"}
	if settings.Guidance != nil {
		fields = append(fields, "guidance")
	}
	fields = append(fields, "vae", "t5_encoder", "clip_embed", "output_count", "board_id")
	if settings.Strength != nil {
		fields = append(fields, "source_image", "strength")
	}
	if len(settings.Loras) > 0 {
		fields = append(fields, "loras")
	}
	return SyncSettings{Model: settings.ModelKey, PositivePrompt: settings.PositivePrompt, NegativePrompt: settings.NegativePrompt, Width: settings.Width, Height: settings.Height, Steps: settings.Steps, Seed: settings.Seeds[0]}, fields
}

func (adapter sdxlAdapter) resolve(request Request, main ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	return resolveSDXL(request, main, inventory, random, adapter.alignment(), adapter.capabilityEntry(request))
}

func (sdxlAdapter) alignment() int { return 8 }

func (sdxlAdapter) compile(resolved Resolution) (EnqueueRequest, error) {
	return compileSDXL(resolved)
}

func (sdxlAdapter) capabilityEntry(request Request) capability.Entry {
	var entry capability.Entry
	if request.Source != nil {
		entry = capability.SDXLImageToImageEntry()
	} else {
		entry = capability.SDXLGenerationEntry()
	}
	if len(request.Loras) > 0 {
		return capability.WithSDXLLoRA(entry)
	}
	return entry
}

func (sdxlAdapter) componentKeys(resolved Resolution) map[string]string {
	keys := map[string]string{}
	if resolved.Models.VAE.Key != "" {
		keys["vae"] = resolved.Models.VAE.Key
	}
	return keys
}

func (adapter sdxlAdapter) validateRecall(_ ModelIdentifier, width, height, steps *int) error {
	return validateAlignedRecall(width, height, steps, adapter.alignment())
}

func (sdxlAdapter) synchronization(settings ResolvedSettings) (SyncSettings, []string) {
	patch := SyncSettings{
		Model: settings.ModelKey, PositivePrompt: settings.PositivePrompt, NegativePrompt: settings.NegativePrompt,
		Width: settings.Width, Height: settings.Height, Steps: settings.Steps, Seed: settings.Seeds[0],
		Additional: []capability.RecallPatchField{{Requirement: capability.SDXLCFGRecallField, Value: settings.Guidance}},
	}
	fields := []string{"scheduler", "vae", "output_count", "board_id"}
	if settings.Strength != nil {
		fields = append(fields, "source_image", "strength")
	}
	if len(settings.Loras) > 0 {
		fields = append(fields, "loras")
	}
	return patch, fields
}

func (adapter animaAdapter) resolve(request Request, main ModelIdentifier, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	return resolveAnima(request, main, inventory, random, adapter.alignment(), adapter.capabilityEntry(request))
}

func (animaAdapter) alignment() int { return 8 }

func (animaAdapter) compile(resolved Resolution) (EnqueueRequest, error) {
	return compileAnima(resolved)
}

func (animaAdapter) capabilityEntry(request Request) capability.Entry {
	var entry capability.Entry
	if request.Source != nil {
		entry = capability.AnimaImageToImageEntry()
	} else {
		entry = capability.AnimaGenerationEntry()
	}
	if len(request.Loras) > 0 {
		return capability.WithAnimaLoRA(entry)
	}
	return entry
}

func (animaAdapter) componentKeys(resolved Resolution) map[string]string {
	return map[string]string{"vae": resolved.Models.VAE.Key, "qwen3_encoder": resolved.Models.Qwen3Encoder.Key}
}

func (adapter animaAdapter) validateRecall(_ ModelIdentifier, width, height, steps *int) error {
	return validateAlignedRecall(width, height, steps, adapter.alignment())
}

func validateGenerationDimensions(width, height, alignment int) error {
	if width < 1 || width%alignment != 0 || height < 1 || height%alignment != 0 {
		return operation.InvalidRequest(fmt.Sprintf("width and height must be positive multiples of %d", alignment))
	}
	return nil
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
	fields := []string{"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id"}
	if settings.Strength != nil {
		fields = append(fields, "source_image", "strength")
	}
	if len(settings.Loras) > 0 {
		fields = append(fields, "loras")
	}
	return SyncSettings{
		Model: settings.ModelKey, PositivePrompt: settings.PositivePrompt, NegativePrompt: settings.NegativePrompt,
		Width: settings.Width, Height: settings.Height, Steps: settings.Steps, Seed: settings.Seeds[0],
	}, fields
}

func adapterForBase(base string) (familyAdapter, error) {
	adapter, ok := families[base]
	if !ok {
		return nil, operation.UnsupportedCapability(fmt.Sprintf("model family %q is not supported for generation", base))
	}
	return adapter, nil
}

func validateProfileApplicability(name string, profile profiles.Generate, main ModelIdentifier) error {
	adapter, err := adapterForBase(main.Base)
	if err != nil {
		return err
	}
	rules := adapter.profileApplicability(main)
	invalid := func(field string) error { return &ProfileSettingError{Profile: name, Field: field} }
	if profile.Components != nil {
		for _, component := range []struct {
			kind     string
			selector *string
		}{
			{"vae", profile.Components.VAE},
			{"qwen3_encoder", profile.Components.Qwen3Encoder},
			{"t5_encoder", profile.Components.T5Encoder},
			{"clip_embed", profile.Components.CLIPEmbed},
		} {
			if component.selector != nil && !slices.Contains(rules.components, component.kind) {
				return invalid(component.kind)
			}
		}
	}
	if profile.Width != nil {
		if *profile.Width%adapter.alignment() != 0 {
			return invalid("width")
		}
		if *profile.Height%adapter.alignment() != 0 {
			return invalid("height")
		}
	}
	if profile.Guidance != nil && !rules.guidance {
		return invalid("guidance")
	}
	if profile.Scheduler != nil && !adapter.capabilityEntry(Request{}).SupportsScheduler(*profile.Scheduler) {
		return invalid("scheduler")
	}
	return nil
}

// Resolve applies the selected family's defaults and resolves its required
// installed models without consulting browser state.
func Resolve(request Request, inventory []ModelIdentifier, random io.Reader) (Resolution, error) {
	if err := validateCommonRequest(request); err != nil {
		return Resolution{}, err
	}
	main, err := ResolveFamilyMain(inventory, request.Model)
	if err != nil {
		return Resolution{}, err
	}
	adapter, err := adapterForBase(main.Base)
	if err != nil {
		return Resolution{}, err
	}
	if err := validateLoRAFamily(request.Loras, main.Base); err != nil {
		return Resolution{}, err
	}
	return adapter.resolve(applyModeDefaults(request), main, inventory, random)
}

// Compile produces the selected family's tested InvokeAI enqueue graph.
func Compile(resolved Resolution) (EnqueueRequest, error) {
	adapter, err := adapterForBase(resolved.Models.Main.Base)
	if err != nil {
		return EnqueueRequest{}, err
	}
	return adapter.compile(resolved)
}

// CapabilityEntry supplies the tested requirements for the resolved family
// and the Generation Mode inferred from its Source Image.
func CapabilityEntry(resolved Resolution) (capability.Entry, error) {
	adapter, err := adapterForBase(resolved.Models.Main.Base)
	if err != nil {
		return capability.Entry{}, err
	}
	return adapter.capabilityEntry(resolved.Request), nil
}

// ResolveFamilyMain selects an installed main model, then applies the
// generation family registry. Other graph operations use graphops.ResolveMain.
func ResolveFamilyMain(inventory []ModelIdentifier, selector string) (ModelIdentifier, error) {
	model, err := graphops.ResolveMain(inventory, selector)
	if err != nil {
		return ModelIdentifier{}, err
	}
	if _, err := adapterForBase(model.Base); err != nil {
		return ModelIdentifier{}, err
	}
	return graphops.CompleteModelIdentifier(model)
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
