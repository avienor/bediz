package upscale

import (
	"errors"
	"fmt"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
)

// ProfileLoadError distinguishes a missing or invalid local profile from an
// InvokeAI failure.
type ProfileLoadError struct {
	Name string
	Err  error
}

func (e *ProfileLoadError) Error() string { return fmt.Sprintf("load profile %q: %v", e.Name, e.Err) }
func (e *ProfileLoadError) Unwrap() error { return e.Err }

// ProfilePreferenceError retains preference warnings when normal resolution
// cannot proceed, such as when no Tile ControlNet was selected.
type ProfilePreferenceError struct {
	Err      error
	Warnings []result.Warning
}

func (e *ProfilePreferenceError) Error() string { return e.Err.Error() }
func (e *ProfilePreferenceError) Unwrap() error { return e.Err }

func withProfileWarnings(err error, warnings []result.Warning) error {
	if err == nil || len(warnings) == 0 {
		return err
	}
	return &ProfilePreferenceError{Err: err, Warnings: warnings}
}

func applyProfileSettings(request Request, profile profiles.Upscale) Request {
	if request.Scale == nil {
		request.Scale = profile.Scale
	}
	if request.Creativity == nil {
		request.Creativity = profile.Creativity
	}
	if request.Structure == nil {
		request.Structure = profile.Structure
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
	if request.TileSize == nil {
		request.TileSize = profile.TileSize
	}
	if request.TileOverlap == nil {
		request.TileOverlap = profile.TileOverlap
	}
	return request
}

func applyProfileComponents(request Request, profile profiles.Upscale, main graphops.ModelIdentifier, inventory []graphops.ModelIdentifier) (Request, []result.Warning) {
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
		{"upscale_model", profile.Components.UpscaleModel, &resolved.UpscaleModel, graphops.ComponentRequirement{Kind: "upscale_model", Base: "any", ModelType: "spandrel_image_to_image"}},
		{"tile_controlnet", profile.Components.TileControlNet, &resolved.TileControlNet, graphops.ComponentRequirement{Kind: "tile_controlnet", Base: main.Base, ModelType: "controlnet"}},
		{"vae", profile.Components.VAE, &resolved.VAE, graphops.ComponentRequirement{Kind: "vae", Base: main.Base, ModelType: "vae"}},
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
			Message: fmt.Sprintf("profile %q %s preference could not be used; ordinary component resolution continues", request.Profile, preference.kind),
			Details: map[string]any{"profile": request.Profile, "component": preference.kind, "reason": reason},
		})
	}
	request.Components = &resolved
	return request, warnings
}
