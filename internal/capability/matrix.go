package capability

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/avienor/bediz/internal/result"
)

const SupportedInvokeAIRange = ">= 6.14.1, < 6.15.0"

const RecallEndpoint = "/api/v1/recall/{queue_id}"
const RecallSchemaRef = "#/components/schemas/RecallParameter"

type RecallFieldRequirement struct {
	Name string
	Type string
}

// RecallSchemaAlternative is one type branch in a Recall patch field's anyOf.
type RecallSchemaAlternative struct {
	Type string `json:"type"`
}

// MatchesNullableAlternatives accepts the expected field type and null in
// either order, without accepting extra or duplicate branches.
func (field RecallFieldRequirement) MatchesNullableAlternatives(alternatives []RecallSchemaAlternative) bool {
	return len(alternatives) == 2 &&
		((alternatives[0].Type == field.Type && alternatives[1].Type == "null") ||
			(alternatives[0].Type == "null" && alternatives[1].Type == field.Type))
}

var RecallPatchFields = []RecallFieldRequirement{
	{Name: "positive_prompt", Type: "string"},
	{Name: "negative_prompt", Type: "string"},
	{Name: "model", Type: "string"},
	{Name: "width", Type: "integer"},
	{Name: "height", Type: "integer"},
	{Name: "steps", Type: "integer"},
	{Name: "seed", Type: "integer"},
}

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

type VersionPolicy string

const (
	VersionPolicySupportedRange     VersionPolicy = "supported_range"
	VersionPolicyCompatibleEndpoint VersionPolicy = "compatible_endpoint"
)

type Entry struct {
	Operation     string
	Family        string
	UISync        string
	VersionPolicy VersionPolicy
	Endpoints     []EndpointRequirement
	Invocations   []InvocationRequirement
	Models        []ModelRequirement
}

var Matrix = []Entry{
	{
		Operation:     result.OperationModelsList,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v2/models/"},
		},
	},
	{
		Operation:     result.OperationImagesList,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/images/"},
		},
	},
	{
		Operation:     result.OperationImagesGet,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/images/i/{image_name}"},
		},
	},
	{
		Operation:     result.OperationImagesUpload,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "POST", Path: "/api/v1/images/upload"},
		},
	},
	{
		Operation:     result.OperationQueueList,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/queue/{queue_id}/item_ids"},
			{Method: "POST", Path: "/api/v1/queue/{queue_id}/item_summaries_by_ids"},
		},
	},
	{
		Operation:     result.OperationQueueGet,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/queue/{queue_id}/i/{item_id}"},
			{Method: "GET", Path: "/api/v1/images/i/{image_name}"},
		},
	},
	AnimaGenerationEntry(),
	{
		Operation:     result.OperationRecall,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "POST", Path: RecallEndpoint},
		},
	},
}

// AnimaGenerationEntry returns the tested InvokeAI requirements shared by
// capability reporting and direct execution validation.
func AnimaGenerationEntry() Entry {
	return Entry{
		Operation:     result.OperationGenerate,
		Family:        "anima",
		UISync:        "partial",
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "GET", Path: "/api/v2/models/"},
			{Method: "POST", Path: "/api/v1/queue/{queue_id}/enqueue_batch"},
			{Method: "GET", Path: "/api/v1/queue/{queue_id}/i/{item_id}"},
			{Method: "GET", Path: "/api/v1/images/i/{image_name}"},
		},
		Invocations: []InvocationRequirement{
			{
				Schema: "AnimaModelLoaderInvocation", Type: "anima_model_loader",
				Properties: []string{"id", "is_intermediate", "use_cache", "type", "model", "vae_model", "qwen3_encoder_model"},
			},
			{
				Schema: "StringInvocation", Type: "string",
				Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"},
			},
			{
				Schema: "AnimaTextEncoderInvocation", Type: "anima_text_encoder",
				Properties: []string{"id", "is_intermediate", "use_cache", "type", "prompt", "qwen3_encoder"},
			},
			{
				Schema: "CollectInvocation", Type: "collect",
				Properties: []string{"id", "is_intermediate", "use_cache", "type", "collection", "item"},
			},
			{
				Schema: "IntegerInvocation", Type: "integer",
				Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"},
			},
			{
				Schema: "AnimaDenoiseInvocation", Type: "anima_denoise",
				Properties: []string{
					"id", "is_intermediate", "use_cache", "type", "denoising_start", "denoising_end", "add_noise",
					"guidance_scale", "width", "height", "steps", "seed", "scheduler", "transformer",
					"positive_conditioning", "negative_conditioning",
				},
			},
			{
				Schema: "CoreMetadataInvocation", Type: "core_metadata",
				Properties: []string{
					"id", "is_intermediate", "use_cache", "type", "generation_mode", "negative_prompt", "width", "height",
					"cfg_scale", "steps", "scheduler", "model", "vae", "qwen3_encoder", "seed", "positive_prompt",
				},
			},
			{
				Schema: "AnimaLatentsToImageInvocation", Type: "anima_l2i",
				Properties: []string{"id", "is_intermediate", "use_cache", "type", "board", "latents", "metadata", "vae"},
			},
		},
		Models: []ModelRequirement{
			{Name: "Anima main model", Types: []string{"main"}, Bases: []string{"anima"}, MinimumCount: 1},
			{Name: "Anima VAE", Types: []string{"vae"}, Bases: []string{"anima"}, MinimumCount: 1},
			{Name: "Qwen3 encoder", Types: []string{"qwen3_encoder"}, Bases: []string{"any"}, MinimumCount: 1},
		},
	}
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
	for identifier := range strings.SplitSeq(matches[versionPattern.SubexpIndex("prerelease")], ".") {
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
