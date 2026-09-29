package capability

import (
	"encoding/json/v2"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/avienor/bediz/internal/result"
)

const SupportedInvokeAIRange = ">= 6.14.1, < 6.15.0"

const RecallEndpoint = "/api/v1/recall/{queue_id}"
const RecallSchemaRef = "#/components/schemas/RecallParameter"
const HuggingFaceAuthEndpoint = "/api/v2/models/hf_login"

// HasHuggingFaceTokenBody checks the tested login request contract shared by
// mutation preflight and doctor capability reporting.
func HasHuggingFaceTokenBody(post []byte, tokenType string, required []string) bool {
	var endpoint struct {
		RequestBody struct {
			Content map[string]struct {
				Schema struct {
					Ref string `json:"$ref"`
				} `json:"schema"`
			} `json:"content"`
		} `json:"requestBody"`
	}
	if err := json.Unmarshal(post, &endpoint); err != nil {
		return false
	}
	return endpoint.RequestBody.Content["application/json"].Schema.Ref == "#/components/schemas/Body_do_hf_login" && tokenType == "string" && slices.Contains(required, "token")
}

type RecallFieldRequirement struct {
	Name string
	Type string
}

// RecallPatchField is an adapter-owned field paired with its tested schema.
type RecallPatchField struct {
	Requirement RecallFieldRequirement
	Value       any
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

var SDXLCFGRecallField = RecallFieldRequirement{Name: "cfg_scale", Type: "number"}

type EndpointRequirement struct {
	Method string
	Path   string
}

// InstallEndpoint describes the OpenAPI parameters required by the tested
// generic model installation route.
type InstallEndpoint struct {
	Parameters []struct {
		Name     string `json:"name"`
		In       string `json:"in"`
		Required bool   `json:"required"`
	} `json:"parameters"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema struct {
				Ref string `json:"$ref"`
			} `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
}

func (endpoint InstallEndpoint) HasRequiredSource() bool {
	for _, parameter := range endpoint.Parameters {
		if parameter.Name == "source" && parameter.In == "query" && parameter.Required {
			return true
		}
	}
	return false
}

func (endpoint InstallEndpoint) HasAccessTokenQuery() bool {
	for _, parameter := range endpoint.Parameters {
		if parameter.Name == "access_token" && parameter.In == "query" {
			return true
		}
	}
	return false
}

func (endpoint InstallEndpoint) HasInplaceQuery() bool {
	for _, parameter := range endpoint.Parameters {
		if parameter.Name == "inplace" && parameter.In == "query" {
			return true
		}
	}
	return false
}

func (endpoint InstallEndpoint) HasJobResponse() bool {
	return endpoint.Responses["201"].Content["application/json"].Schema.Ref == "#/components/schemas/ModelInstallJob"
}

func (endpoint InstallEndpoint) HasStarterCatalogResponse() bool {
	return endpoint.Responses["200"].Content["application/json"].Schema.Ref == "#/components/schemas/StarterModelResponse"
}

type InvocationRequirement struct {
	Schema                       string
	Type                         string
	Properties                   []string
	RequiresAdditionalProperties bool
}

type ModelRequirement struct {
	Name         string
	Types        []string
	Bases        []string
	Variants     []string
	Formats      []string
	MinimumCount int
}

// InvokeAI 6.14.1's FLUX model loader accepts these non-SDNQ main config
// formats for text-to-image. Its ordinary Diffusers config is not supported.
var fluxMainFormats = []string{"checkpoint", "bnb_quantized_nf4b", "gguf_quantized"}

var fluxMainVariants = []string{"dev", "schnell"}

func SupportsFLUXMain(variant, format string) bool {
	return slices.Contains(fluxMainVariants, variant) && slices.Contains(fluxMainFormats, format)
}

type VersionPolicy string

const (
	VersionPolicySupportedRange     VersionPolicy = "supported_range"
	VersionPolicyCompatibleEndpoint VersionPolicy = "compatible_endpoint"
)

type SpecialPredicate string

const (
	RecallPatchSchema       SpecialPredicate = "recall_patch_schema"
	InstallSchema           SpecialPredicate = "install_schema"
	InstallInplaceQuery     SpecialPredicate = "install_inplace_query"
	InstallAccessTokenQuery SpecialPredicate = "install_access_token_query"
	StarterCatalogResponse  SpecialPredicate = "starter_catalog_response"
	HuggingFaceLoginBody    SpecialPredicate = "hugging_face_login_body"
)

type Entry struct {
	Operation     string
	Family        string
	Mode          string
	UISync        string
	VersionPolicy VersionPolicy
	Endpoints     []EndpointRequirement
	Invocations   []InvocationRequirement
	Models        []ModelRequirement
	Schedulers    []string
	Special       []SpecialPredicate
	RecallFields  []RecallFieldRequirement
}

// SupportsScheduler checks the scheduler set tested for this family.
func (entry Entry) SupportsScheduler(name string) bool {
	return slices.Contains(entry.Schedulers, name)
}

// IsRegisteredScheduler accepts the union of all tested graph-operation sets.
// Profile save-time validation has no resolved Model Family.
func IsRegisteredScheduler(name string) bool {
	for _, entry := range Matrix {
		if (entry.Operation == result.OperationGenerate || entry.Operation == result.OperationUpscale) && entry.SupportsScheduler(name) {
			return true
		}
	}
	return false
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
		Operation:     result.OperationModelsScan,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v2/models/scan_folder"},
		},
	},
	{
		Operation:     result.OperationModelsInstall,
		VersionPolicy: VersionPolicySupportedRange,
		Special:       []SpecialPredicate{InstallSchema},
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "POST", Path: "/api/v2/models/install"},
		},
	},
	{
		Operation:     result.OperationModelsInstall,
		Family:        "starter",
		VersionPolicy: VersionPolicySupportedRange,
		Special:       []SpecialPredicate{InstallSchema, StarterCatalogResponse},
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "GET", Path: "/api/v2/models/starter_models"},
			{Method: "POST", Path: "/api/v2/models/install"},
		},
	},
	installSourceEntry("huggingface", []EndpointRequirement{{Method: "GET", Path: "/api/v2/models/hugging_face"}}, nil),
	installSourceEntry("path", nil, []SpecialPredicate{InstallInplaceQuery}),
	installSourceEntry("source_token", nil, []SpecialPredicate{InstallAccessTokenQuery}),
	{
		Operation:     result.OperationModelsStatus,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v2/models/install/{id}"},
		},
	},
	{
		Operation:     result.OperationModelsDelete,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "GET", Path: "/api/v2/models/i/{key}"},
			{Method: "DELETE", Path: "/api/v2/models/i/{key}"},
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
		Operation:     result.OperationImagesDownload,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/images/i/{image_name}/full"},
		},
	},
	{
		Operation:     result.OperationImagesDelete,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "DELETE", Path: "/api/v1/images/i/{image_name}"},
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
	{
		Operation:     result.OperationQueueWait,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/queue/{queue_id}/i/{item_id}"},
			{Method: "GET", Path: "/api/v1/images/i/{image_name}"},
		},
	},
	{
		Operation:     result.OperationQueueCancel,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "PUT", Path: "/api/v1/queue/{queue_id}/i/{item_id}/cancel"},
			{Method: "GET", Path: "/api/v1/images/i/{image_name}"},
		},
	},
	{
		Operation:     result.OperationQueueClear,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "PUT", Path: "/api/v1/queue/{queue_id}/clear"},
		},
	},
	{
		Operation:     result.OperationBoardsList,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/boards/"},
		},
	},
	{
		Operation:     result.OperationBoardsGet,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/boards/{board_id}"},
			{Method: "GET", Path: "/api/v1/boards/"},
		},
	},
	{
		Operation:     result.OperationBoardsCreate,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "GET", Path: "/api/v1/boards/"},
			{Method: "POST", Path: "/api/v1/boards/"},
		},
	},
	AnimaGenerationEntry(),
	AnimaImageToImageEntry(),
	SDXLGenerationEntry(),
	SDXLImageToImageEntry(),
	FLUXGenerationEntry(),
	FLUXImageToImageEntry(),
	SDXLUpscaleEntry(),
	SD1UpscaleEntry(),
	RecallEntry([]RecallFieldRequirement{SDXLCFGRecallField}),
	{
		Operation:     result.OperationAuthHFStatus,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints:     []EndpointRequirement{{Method: "GET", Path: HuggingFaceAuthEndpoint}},
	},
	{
		Operation:     result.OperationAuthHFLogin,
		VersionPolicy: VersionPolicySupportedRange,
		Special:       []SpecialPredicate{HuggingFaceLoginBody},
		Endpoints:     []EndpointRequirement{{Method: "POST", Path: HuggingFaceAuthEndpoint}},
	},
	{
		Operation:     result.OperationAuthHFLogout,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints:     []EndpointRequirement{{Method: "DELETE", Path: HuggingFaceAuthEndpoint}},
	},
}

// Find returns the recorded capability entry for an exact operation, family,
// and generation mode.
func Find(operation, family, mode string) (Entry, bool) {
	for _, entry := range Matrix {
		if entry.Operation == operation && entry.Family == family && entry.Mode == mode {
			return entry, true
		}
	}
	return Entry{}, false
}

// InstallPreflightEntries selects the doctor rows that describe one install
// request. Starter entries that resolve to repositories are checked again with
// the Hugging Face row before submission.
func InstallPreflightEntries(sourceType string, hasSourceToken bool) ([]Entry, bool) {
	generic, found := Find(result.OperationModelsInstall, "", "")
	if !found {
		return nil, false
	}
	entries := []Entry{generic}
	switch sourceType {
	case "starter", "huggingface", "path":
		entry, found := Find(result.OperationModelsInstall, sourceType, "")
		if !found {
			return nil, false
		}
		entries = append(entries, entry)
	}
	if hasSourceToken {
		entry, found := Find(result.OperationModelsInstall, "source_token", "")
		if !found {
			return nil, false
		}
		entries = append(entries, entry)
	}
	return entries, true
}

// RecallEntry records the common patch schema and any fields used by a
// generation family's UI Synchronization. The doctor row includes the union.
func RecallEntry(additional []RecallFieldRequirement) Entry {
	return Entry{
		Operation:     result.OperationRecall,
		VersionPolicy: VersionPolicySupportedRange,
		Special:       []SpecialPredicate{RecallPatchSchema},
		Endpoints:     []EndpointRequirement{{Method: "POST", Path: RecallEndpoint}},
		RecallFields:  append(slices.Clone(RecallPatchFields), additional...),
	}
}

func installSourceEntry(family string, additionalEndpoints []EndpointRequirement, additionalPredicates []SpecialPredicate) Entry {
	return Entry{
		Operation:     result.OperationModelsInstall,
		Family:        family,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: append([]EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "POST", Path: "/api/v2/models/install"},
		}, additionalEndpoints...),
		Special: append([]SpecialPredicate{InstallSchema}, additionalPredicates...),
	}
}

// SDXLUpscaleEntry records the tested stock 6.14.1 tiled upscale graph.
// Model requirements establish presence, not that a ControlNet is a Tile model
// or that a Spandrel model enlarges its input.
func SDXLUpscaleEntry() Entry {
	return upscaleEntry("sdxl", "SDXL", []InvocationRequirement{
		{Schema: "SDXLModelLoaderInvocation", Type: "sdxl_model_loader", Properties: []string{"id", "is_intermediate", "use_cache", "type", "model"}},
		{Schema: "SDXLCompelPromptInvocation", Type: "sdxl_compel_prompt", Properties: []string{"id", "is_intermediate", "use_cache", "type", "prompt", "style", "clip", "clip2"}},
	})
}

// SD1UpscaleEntry records the stock 6.14.1 SD1.5 branch of the tiled upscale
// graph. Any main format InvokeAI's SD1.5 loader accepts is supported.
func SD1UpscaleEntry() Entry {
	return upscaleEntry("sd-1", "SD1.5", []InvocationRequirement{
		{Schema: "MainModelLoaderInvocation", Type: "main_model_loader", Properties: []string{"id", "is_intermediate", "use_cache", "type", "model"}},
		{Schema: "CLIPSkipInvocation", Type: "clip_skip", Properties: []string{"id", "is_intermediate", "use_cache", "type", "clip", "skipped_layers"}},
		{Schema: "CompelInvocation", Type: "compel", Properties: []string{"id", "is_intermediate", "use_cache", "type", "prompt", "clip"}},
	})
}

// upscaleEntry requires the image upload endpoint because the upscale contract
// includes local-file sources.
func upscaleEntry(base, label string, conditioning []InvocationRequirement) Entry {
	invocations := []InvocationRequirement{
		{Schema: "StringInvocation", Type: "string", Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"}},
		{Schema: "IntegerInvocation", Type: "integer", Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"}},
		{Schema: "SpandrelImageToImageAutoscaleInvocation", Type: "spandrel_image_to_image_autoscale", Properties: []string{"id", "is_intermediate", "use_cache", "type", "image", "image_to_image_model", "scale", "fit_to_multiple_of_8"}},
		{Schema: "UnsharpMaskInvocation", Type: "unsharp_mask", Properties: []string{"id", "is_intermediate", "use_cache", "type", "image", "radius", "strength"}},
		{Schema: "NoiseInvocation", Type: "noise", Properties: []string{"id", "is_intermediate", "use_cache", "type", "seed", "width", "height", "use_cpu"}},
		{Schema: "ImageToLatentsInvocation", Type: "i2l", Properties: []string{"id", "is_intermediate", "use_cache", "type", "image", "vae", "tiled", "tile_size", "fp32"}},
		{Schema: "LatentsToImageInvocation", Type: "l2i", Properties: []string{"id", "is_intermediate", "use_cache", "type", "latents", "vae", "tiled", "tile_size", "fp32", "board", "metadata"}},
		{Schema: "TiledMultiDiffusionDenoiseLatents", Type: "tiled_multi_diffusion_denoise_latents", Properties: []string{"id", "is_intermediate", "use_cache", "type", "tile_width", "tile_height", "tile_overlap", "steps", "cfg_scale", "scheduler", "denoising_start", "denoising_end", "control"}},
		{Schema: "ControlNetInvocation", Type: "controlnet", Properties: []string{"id", "is_intermediate", "use_cache", "type", "image", "control_model", "control_weight", "begin_step_percent", "end_step_percent", "control_mode", "resize_mode"}},
		{Schema: "CollectInvocation", Type: "collect", Properties: []string{"id", "is_intermediate", "use_cache", "type", "collection", "item"}},
	}
	invocations = append(invocations, conditioning...)
	invocations = append(invocations,
		InvocationRequirement{Schema: "VAELoaderInvocation", Type: "vae_loader", Properties: []string{"id", "is_intermediate", "use_cache", "type", "vae_model"}},
		InvocationRequirement{Schema: "CoreMetadataInvocation", Type: "core_metadata", Properties: []string{"id", "is_intermediate", "use_cache", "type", "positive_prompt", "negative_prompt", "seed", "width", "height", "steps", "scheduler", "cfg_scale", "model", "vae"}, RequiresAdditionalProperties: true},
	)
	return Entry{
		Operation: result.OperationUpscale, Family: base, UISync: "partial", VersionPolicy: VersionPolicySupportedRange,
		Schedulers:  slices.Clone(sdxlSchedulers),
		Endpoints:   append(slices.Clone(AnimaGenerationEntry().Endpoints), EndpointRequirement{Method: "POST", Path: "/api/v1/images/upload"}),
		Invocations: invocations,
		Models: []ModelRequirement{
			{Name: label + " normal main model", Types: []string{"main"}, Bases: []string{base}, Variants: []string{"normal"}, MinimumCount: 1},
			{Name: "Spandrel upscale model", Types: []string{"spandrel_image_to_image"}, Bases: []string{"any"}, MinimumCount: 1},
			{Name: label + " ControlNet", Types: []string{"controlnet"}, Bases: []string{base}, MinimumCount: 1},
		},
	}
}

// FLUXGenerationEntry records the tested stock 6.14.1 FLUX.1 graph.
func FLUXGenerationEntry() Entry {
	return Entry{
		Operation: result.OperationGenerate, Family: "flux", Mode: "txt2img", UISync: "partial", VersionPolicy: VersionPolicySupportedRange,
		Schedulers: []string{"euler", "heun", "lcm"},
		Endpoints:  slices.Clone(AnimaGenerationEntry().Endpoints),
		Invocations: []InvocationRequirement{
			{Schema: "FluxModelLoaderInvocation", Type: "flux_model_loader", Properties: []string{"id", "is_intermediate", "use_cache", "type", "model", "vae_model", "t5_encoder_model", "clip_embed_model"}},
			{Schema: "FluxTextEncoderInvocation", Type: "flux_text_encoder", Properties: []string{"id", "is_intermediate", "use_cache", "type", "clip", "t5_encoder", "t5_max_seq_len", "prompt"}},
			{Schema: "FluxDenoiseInvocation", Type: "flux_denoise", Properties: []string{"id", "is_intermediate", "use_cache", "type", "transformer", "positive_text_conditioning", "cfg_scale", "width", "height", "num_steps", "scheduler", "guidance", "seed"}},
			{Schema: "FluxVaeDecodeInvocation", Type: "flux_vae_decode", Properties: []string{"id", "is_intermediate", "use_cache", "type", "latents", "vae", "metadata", "board"}},
			{Schema: "CoreMetadataInvocation", Type: "core_metadata", Properties: []string{"id", "is_intermediate", "use_cache", "type", "generation_mode", "positive_prompt", "negative_prompt", "seed", "width", "height", "steps", "scheduler", "cfg_scale", "model", "vae"}},
			{Schema: "StringInvocation", Type: "string", Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"}},
			{Schema: "IntegerInvocation", Type: "integer", Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"}},
		},
		Models: []ModelRequirement{
			{Name: "FLUX.1 main model", Types: []string{"main"}, Bases: []string{"flux"}, Variants: slices.Clone(fluxMainVariants), Formats: slices.Clone(fluxMainFormats), MinimumCount: 1},
			{Name: "FLUX.1 VAE", Types: []string{"vae"}, Bases: []string{"flux"}, MinimumCount: 1},
			{Name: "FLUX.1 T5 encoder", Types: []string{"t5_encoder"}, Bases: []string{"any"}, MinimumCount: 1},
			{Name: "FLUX.1 CLIP Embed", Types: []string{"clip_embed"}, Bases: []string{"any"}, MinimumCount: 1},
		},
	}
}

// FLUXImageToImageEntry adds the tested source-image encoder path to FLUX.1 generation.
func FLUXImageToImageEntry() Entry {
	entry := FLUXGenerationEntry()
	entry.Mode = "img2img"
	entry.Endpoints = append(entry.Endpoints, EndpointRequirement{Method: "POST", Path: "/api/v1/images/upload"})
	entry.Invocations = slices.Clone(entry.Invocations)
	for index := range entry.Invocations {
		switch entry.Invocations[index].Type {
		case "flux_denoise":
			entry.Invocations[index].Properties = append(slices.Clone(entry.Invocations[index].Properties), "latents", "denoising_start", "denoising_end", "add_noise")
		case "core_metadata":
			entry.Invocations[index].Properties = append(slices.Clone(entry.Invocations[index].Properties), "strength", "init_image")
		}
	}
	common := []string{"id", "is_intermediate", "use_cache", "type"}
	entry.Invocations = append(entry.Invocations,
		InvocationRequirement{Schema: "ImageResizeInvocation", Type: "img_resize", Properties: append(slices.Clone(common), "image", "width", "height", "resample_mode")},
		InvocationRequirement{Schema: "FluxVaeEncodeInvocation", Type: "flux_vae_encode", Properties: append(slices.Clone(common), "image", "vae")},
	)
	return entry
}

// SDXL generation and both stock upscale families share this tested set.
var sdxlSchedulers = []string{
	"ddim", "ddpm", "deis", "deis_k", "lms", "lms_k", "pndm", "heun", "heun_k", "euler", "euler_k", "euler_a",
	"kdpm_2", "kdpm_2_k", "kdpm_2_a", "kdpm_2_a_k", "dpmpp_2s", "dpmpp_2s_k", "dpmpp_2m", "dpmpp_2m_k",
	"dpmpp_2m_sde", "dpmpp_2m_sde_k", "dpmpp_3m", "dpmpp_3m_k", "dpmpp_sde", "dpmpp_sde_k", "er_sde",
	"unipc", "unipc_k", "lcm", "tcd",
}

// SDXLGenerationEntry records the tested stock 6.14.1 text-to-image graph.
func SDXLGenerationEntry() Entry {
	return Entry{
		Operation: result.OperationGenerate, Family: "sdxl", Mode: "txt2img", UISync: "partial", VersionPolicy: VersionPolicySupportedRange,
		Schedulers: slices.Clone(sdxlSchedulers),
		Endpoints:  slices.Clone(AnimaGenerationEntry().Endpoints),
		Invocations: []InvocationRequirement{
			{Schema: "SDXLModelLoaderInvocation", Type: "sdxl_model_loader", Properties: []string{"id", "is_intermediate", "use_cache", "type", "model"}},
			{Schema: "StringInvocation", Type: "string", Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"}},
			{Schema: "SDXLCompelPromptInvocation", Type: "sdxl_compel_prompt", Properties: []string{"id", "is_intermediate", "use_cache", "type", "prompt", "style", "clip", "clip2"}},
			{Schema: "CollectInvocation", Type: "collect", Properties: []string{"id", "is_intermediate", "use_cache", "type", "collection", "item"}},
			{Schema: "IntegerInvocation", Type: "integer", Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"}},
			{Schema: "NoiseInvocation", Type: "noise", Properties: []string{"id", "is_intermediate", "use_cache", "type", "seed", "width", "height", "use_cpu"}},
			{Schema: "DenoiseLatentsInvocation", Type: "denoise_latents", Properties: []string{"id", "is_intermediate", "use_cache", "type", "unet", "positive_conditioning", "negative_conditioning", "noise", "steps", "scheduler", "cfg_scale", "cfg_rescale_multiplier", "denoising_start", "denoising_end"}},
			{Schema: "LatentsToImageInvocation", Type: "l2i", Properties: []string{"id", "is_intermediate", "use_cache", "type", "latents", "vae", "fp32", "metadata", "board"}},
			{Schema: "VAELoaderInvocation", Type: "vae_loader", Properties: []string{"id", "is_intermediate", "use_cache", "type", "vae_model"}},
			{Schema: "CoreMetadataInvocation", Type: "core_metadata", Properties: []string{"id", "is_intermediate", "use_cache", "type", "generation_mode", "positive_prompt", "negative_prompt", "seed", "width", "height", "steps", "scheduler", "cfg_scale", "cfg_rescale_multiplier", "rand_device", "model", "vae"}},
		},
		Models: []ModelRequirement{{Name: "SDXL main model", Types: []string{"main"}, Bases: []string{"sdxl"}, MinimumCount: 1}},
	}
}

// SDXLImageToImageEntry adds the tested source-image encoder path to SDXL generation.
func SDXLImageToImageEntry() Entry {
	entry := SDXLGenerationEntry()
	entry.Mode = "img2img"
	entry.Endpoints = append(entry.Endpoints, EndpointRequirement{Method: "POST", Path: "/api/v1/images/upload"})
	entry.Invocations = slices.Clone(entry.Invocations)
	for index := range entry.Invocations {
		switch entry.Invocations[index].Type {
		case "denoise_latents":
			entry.Invocations[index].Properties = append(slices.Clone(entry.Invocations[index].Properties), "latents")
		case "core_metadata":
			entry.Invocations[index].Properties = append(slices.Clone(entry.Invocations[index].Properties), "strength", "init_image")
		}
	}
	common := []string{"id", "is_intermediate", "use_cache", "type"}
	entry.Invocations = append(entry.Invocations,
		InvocationRequirement{Schema: "ImageResizeInvocation", Type: "img_resize", Properties: append(slices.Clone(common), "image", "width", "height", "resample_mode")},
		InvocationRequirement{Schema: "ImageToLatentsInvocation", Type: "i2l", Properties: append(slices.Clone(common), "image", "vae", "fp32", "color_compensation")},
	)
	return entry
}

// AnimaImageToImageEntry adds the tested source-image encoder path to Anima generation.
func AnimaImageToImageEntry() Entry {
	entry := AnimaGenerationEntry()
	entry.Mode = "img2img"
	entry.Endpoints = append(entry.Endpoints, EndpointRequirement{Method: "POST", Path: "/api/v1/images/upload"})
	entry.Invocations = slices.Clone(entry.Invocations)
	for index := range entry.Invocations {
		switch entry.Invocations[index].Type {
		case "anima_denoise":
			entry.Invocations[index].Properties = append(slices.Clone(entry.Invocations[index].Properties), "latents")
		case "core_metadata":
			entry.Invocations[index].Properties = append(slices.Clone(entry.Invocations[index].Properties), "strength", "init_image")
		}
	}
	common := []string{"id", "is_intermediate", "use_cache", "type"}
	entry.Invocations = append(entry.Invocations,
		InvocationRequirement{Schema: "ImageResizeInvocation", Type: "img_resize", Properties: append(slices.Clone(common), "image", "width", "height", "resample_mode")},
		InvocationRequirement{Schema: "AnimaImageToLatentsInvocation", Type: "anima_i2l", Properties: append(slices.Clone(common), "image", "vae")},
	)
	return entry
}

// AnimaGenerationEntry returns the tested InvokeAI requirements shared by
// capability reporting and direct execution validation.
func AnimaGenerationEntry() Entry {
	return Entry{
		Operation:     result.OperationGenerate,
		Family:        "anima",
		Mode:          "txt2img",
		UISync:        "partial",
		VersionPolicy: VersionPolicySupportedRange,
		Schedulers:    []string{"euler", "heun", "dpmpp_2m", "dpmpp_2m_sde", "er_sde", "lcm"},
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
