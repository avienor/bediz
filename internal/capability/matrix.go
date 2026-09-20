package capability

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const SupportedInvokeAIRange = ">= 6.14.1, < 6.15.0"

type EndpointRequirement struct {
	Method string
	Path   string
}

type InvocationRequirement struct {
	Schema     string
	Type       string
	Properties []string
}

type ModelRequirement struct {
	Name         string
	Types        []string
	Bases        []string
	MinimumCount int
}

type Entry struct {
	Operation   string
	Family      string
	UISync      string
	Endpoints   []EndpointRequirement
	Invocations []InvocationRequirement
	Models      []ModelRequirement
}

var Matrix = []Entry{
	{
		Operation: "generate",
		Family:    "anima",
		UISync:    "full",
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "GET", Path: "/api/v2/models/"},
			{Method: "POST", Path: "/api/v1/queue/{queue_id}/enqueue_batch"},
			{Method: "GET", Path: "/api/v1/queue/{queue_id}/status"},
			{Method: "GET", Path: "/api/v1/queue/{queue_id}/i/{item_id}"},
			{Method: "POST", Path: "/api/v1/recall/{queue_id}"},
		},
		Invocations: []InvocationRequirement{
			{
				Schema:     "AnimaModelLoaderInvocation",
				Type:       "anima_model_loader",
				Properties: []string{"model", "vae_model", "qwen3_encoder_model"},
			},
			{
				Schema:     "AnimaTextEncoderInvocation",
				Type:       "anima_text_encoder",
				Properties: []string{"prompt", "qwen3_encoder"},
			},
			{
				Schema: "AnimaDenoiseInvocation",
				Type:   "anima_denoise",
				Properties: []string{
					"transformer", "positive_conditioning", "guidance_scale",
					"width", "height", "steps", "seed", "scheduler",
				},
			},
			{
				Schema:     "AnimaLatentsToImageInvocation",
				Type:       "anima_l2i",
				Properties: []string{"latents", "vae", "board", "metadata"},
			},
			{
				Schema: "CoreMetadataInvocation",
				Type:   "core_metadata",
				Properties: []string{
					"generation_mode", "positive_prompt", "width", "height",
					"seed", "steps", "scheduler", "model", "vae", "qwen3_encoder",
				},
			},
		},
		Models: []ModelRequirement{
			{Name: "Anima main model", Types: []string{"main"}, Bases: []string{"anima"}, MinimumCount: 1},
			{Name: "Anima-compatible VAE", Types: []string{"vae"}, Bases: []string{"anima", "flux", "any"}, MinimumCount: 1},
			{Name: "Qwen3 text encoder", Types: []string{"qwen3_encoder"}, Bases: []string{"anima", "any"}, MinimumCount: 1},
		},
	},
}

var versionPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?P<prerelease>[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func SupportsInvokeAI(version string) (bool, error) {
	major, minor, patch, err := parseVersion(version)
	if err != nil {
		return false, err
	}
	matches := versionPattern.FindStringSubmatch(version)
	if matches[versionPattern.SubexpIndex("prerelease")] != "" {
		return false, nil
	}
	return major == 6 && minor == 14 && patch >= 1, nil
}

func parseVersion(value string) (int, int, int, error) {
	matches := versionPattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, 0, 0, fmt.Errorf("invalid InvokeAI version %q", value)
	}
	for _, identifier := range strings.Split(matches[versionPattern.SubexpIndex("prerelease")], ".") {
		if len(identifier) > 1 && identifier[0] == '0' && isNumericIdentifier(identifier) {
			return 0, 0, 0, fmt.Errorf("invalid InvokeAI version %q", value)
		}
	}
	values := make([]int, 3)
	for i := range values {
		parsed, err := strconv.Atoi(matches[i+1])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid InvokeAI version %q", value)
		}
		values[i] = parsed
	}
	return values[0], values[1], values[2], nil
}

func isNumericIdentifier(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
