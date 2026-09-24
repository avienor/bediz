package generation

import (
	"errors"
	"fmt"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
)

// ProfileLoadError keeps local profile errors separate from InvokeAI failures.
type ProfileLoadError struct {
	Name string
	Err  error
}

func (e *ProfileLoadError) Error() string { return fmt.Sprintf("load profile %q: %v", e.Name, e.Err) }
func (e *ProfileLoadError) Unwrap() error { return e.Err }

func profileLoadError(name string, err error) error {
	return &ProfileLoadError{Name: name, Err: err}
}

// ProfileSettingError identifies a saved setting that cannot apply to the
// selected main model, even if the request supplied an explicit override.
type ProfileSettingError struct {
	Profile string
	Field   string
}

func (e *ProfileSettingError) Error() string {
	return fmt.Sprintf("profile %q setting %q is not applicable to the selected model", e.Profile, e.Field)
}

func validateProfileApplicability(name string, profile profiles.Generate, main ModelIdentifier) error {
	invalid := func(field string) error { return &ProfileSettingError{Profile: name, Field: field} }
	if profile.Components != nil {
		for _, component := range []struct {
			kind     string
			selector *string
			applies  bool
		}{
			{"qwen3_encoder", profile.Components.Qwen3Encoder, main.Base == "anima"},
			{"t5_encoder", profile.Components.T5Encoder, main.Base == "flux"},
			{"clip_embed", profile.Components.CLIPEmbed, main.Base == "flux"},
		} {
			if component.selector != nil && !component.applies {
				return invalid(component.kind)
			}
		}
	}
	if profile.Width != nil {
		multiple := 8
		if main.Base == "flux" {
			multiple = 16
		}
		if *profile.Width%multiple != 0 {
			return invalid("width")
		}
		if *profile.Height%multiple != 0 {
			return invalid("height")
		}
	}
	if profile.Guidance != nil && main.Base == "flux" && main.Variant == "schnell" {
		return invalid("guidance")
	}
	if profile.Scheduler != nil {
		scheduler := *profile.Scheduler
		var supported bool
		switch main.Base {
		case "anima":
			supported = isAnimaScheduler(scheduler)
		case "sdxl":
			supported = graphops.IsSDXLScheduler(scheduler)
		case "flux":
			supported = isFLUXScheduler(scheduler)
		}
		if !supported {
			return invalid("scheduler")
		}
	}
	return nil
}

func applyProfileSettings(request Request, profile profiles.Generate) Request {
	if request.Width == nil {
		request.Width, request.Height = profile.Width, profile.Height
	}
	if request.Steps == nil {
		request.Steps = profile.Steps
	}
	if request.Scheduler == nil {
		request.Scheduler = profile.Scheduler
	}
	if request.Guidance == nil {
		request.Guidance = profile.Guidance
	}
	if request.OutputCount == nil {
		request.OutputCount = profile.OutputCount
	}
	return request
}

func applyProfileComponents(request Request, profile profiles.Generate, main ModelIdentifier, inventory []ModelIdentifier) (Request, []result.Warning) {
	if profile.Components == nil {
		return request, nil
	}
	resolved := Components{}
	if request.Components != nil {
		resolved = *request.Components
	}
	var warnings []result.Warning
	preferences := []struct {
		kind        string
		selector    *string
		selected    **string
		requirement graphops.ComponentRequirement
	}{
		{"vae", profile.Components.VAE, &resolved.VAE, graphops.ComponentRequirement{Kind: "vae", Base: main.Base, ModelType: "vae"}},
		{"qwen3_encoder", profile.Components.Qwen3Encoder, &resolved.Qwen3Encoder, qwen3EncoderRequirement},
		{"t5_encoder", profile.Components.T5Encoder, &resolved.T5Encoder, fluxT5Requirement},
		{"clip_embed", profile.Components.CLIPEmbed, &resolved.CLIPEmbed, fluxCLIPRequirement},
	}
	for _, preference := range preferences {
		if preference.selector == nil || *preference.selected != nil {
			continue
		}
		component, err := graphops.ResolveUniqueCompatible(inventory, *preference.selector, preference.requirement)
		if err == nil {
			*preference.selected = new(component.Key)
			continue
		}
		reason := "not_found"
		if _, ok := errors.AsType[*operation.SelectionRequiredError](err); ok {
			reason = "ambiguous"
		} else if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); ok {
			reason = "incompatible"
		}
		warnings = append(warnings, result.Warning{
			Code:    "profile_preference_skipped",
			Message: fmt.Sprintf("profile %q %s preference could not be used; automatic component resolution continues", request.Profile, preference.kind),
			Details: map[string]any{"profile": request.Profile, "component": preference.kind, "reason": reason},
		})
	}
	request.Components = &resolved
	return request, warnings
}
