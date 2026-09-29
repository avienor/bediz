package generation

import (
	"fmt"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profileexecution"
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

func profileLoadError(name string, err error) error { return &ProfileLoadError{Name: name, Err: err} }

// ProfilePreferenceError retains skipped component preferences when a later
// step fails before an accepted enqueue.
type ProfilePreferenceError = operation.ProfilePreferenceError

// ProfileSettingError identifies a saved setting that cannot apply to the
// selected main model, even if the request supplied an explicit override.
type ProfileSettingError struct {
	Profile string
	Field   string
}

func (e *ProfileSettingError) Error() string {
	return fmt.Sprintf("profile %q setting %q is not applicable to the selected model", e.Profile, e.Field)
}

func applyProfileSettings(request Request, profile profiles.Generate) Request {
	if request.Width == nil && request.Source == nil {
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
	preferences := []profileexecution.Preference{
		{Kind: "vae", Selector: profile.Components.VAE, Selected: &resolved.VAE, Requirement: graphops.ComponentRequirement{Kind: "vae", Base: main.Base, ModelType: "vae"}},
		{Kind: "qwen3_encoder", Selector: profile.Components.Qwen3Encoder, Selected: &resolved.Qwen3Encoder, Requirement: qwen3EncoderRequirement},
		{Kind: "t5_encoder", Selector: profile.Components.T5Encoder, Selected: &resolved.T5Encoder, Requirement: fluxT5Requirement},
		{Kind: "clip_embed", Selector: profile.Components.CLIPEmbed, Selected: &resolved.CLIPEmbed, Requirement: fluxCLIPRequirement},
	}
	warnings := profileexecution.ApplyPreferences(request.Profile, inventory, "automatic component resolution continues", preferences)
	request.Components = &resolved
	return request, warnings
}
