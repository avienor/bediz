package upscale

import (
	"fmt"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profileexecution"
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

func profileLoadError(name string, err error) error { return &ProfileLoadError{Name: name, Err: err} }

// ProfilePreferenceError retains preference warnings when normal resolution
// cannot proceed, such as when no Tile ControlNet was selected.
type ProfilePreferenceError = operation.ProfilePreferenceError

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
	preferences := []profileexecution.Preference{
		{Kind: "upscale_model", Selector: profile.Components.UpscaleModel, Selected: &resolved.UpscaleModel, Requirement: graphops.ComponentRequirement{Kind: "upscale_model", Base: "any", ModelType: "spandrel_image_to_image"}},
		{Kind: "tile_controlnet", Selector: profile.Components.TileControlNet, Selected: &resolved.TileControlNet, Requirement: graphops.ComponentRequirement{Kind: "tile_controlnet", Base: main.Base, ModelType: "controlnet"}},
		{Kind: "vae", Selector: profile.Components.VAE, Selected: &resolved.VAE, Requirement: graphops.ComponentRequirement{Kind: "vae", Base: main.Base, ModelType: "vae"}},
	}
	warnings := profileexecution.ApplyPreferences(request.Profile, inventory, "ordinary component resolution continues", preferences)
	request.Components = &resolved
	return request, warnings
}
