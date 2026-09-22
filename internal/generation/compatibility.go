package generation

import (
	"fmt"
	"strings"

	"github.com/avienor/bediz/internal/operation"
)

type invocationRequirement struct {
	schema     string
	typeName   string
	properties []string
}

var animaInvocationRequirements = []invocationRequirement{
	{
		schema: "AnimaModelLoaderInvocation", typeName: "anima_model_loader",
		properties: []string{"id", "is_intermediate", "use_cache", "type", "model", "vae_model", "qwen3_encoder_model"},
	},
	{
		schema: "StringInvocation", typeName: "string",
		properties: []string{"id", "is_intermediate", "use_cache", "type", "value"},
	},
	{
		schema: "AnimaTextEncoderInvocation", typeName: "anima_text_encoder",
		properties: []string{"id", "is_intermediate", "use_cache", "type", "prompt", "qwen3_encoder"},
	},
	{
		schema: "CollectInvocation", typeName: "collect",
		properties: []string{"id", "is_intermediate", "use_cache", "type", "collection", "item"},
	},
	{
		schema: "IntegerInvocation", typeName: "integer",
		properties: []string{"id", "is_intermediate", "use_cache", "type", "value"},
	},
	{
		schema: "AnimaDenoiseInvocation", typeName: "anima_denoise",
		properties: []string{
			"id", "is_intermediate", "use_cache", "type", "denoising_start", "denoising_end", "add_noise",
			"guidance_scale", "width", "height", "steps", "seed", "scheduler", "transformer",
			"positive_conditioning", "negative_conditioning",
		},
	},
	{
		schema: "CoreMetadataInvocation", typeName: "core_metadata",
		properties: []string{
			"id", "is_intermediate", "use_cache", "type", "generation_mode", "negative_prompt", "width", "height",
			"cfg_scale", "steps", "scheduler", "model", "vae", "qwen3_encoder", "seed", "positive_prompt",
		},
	},
	{
		schema: "AnimaLatentsToImageInvocation", typeName: "anima_l2i",
		properties: []string{"id", "is_intermediate", "use_cache", "type", "board", "latents", "metadata", "vae"},
	},
}

type openAPIDocument struct {
	Components struct {
		Schemas map[string]openAPISchema `json:"schemas"`
	} `json:"components"`
}

type openAPISchema struct {
	Properties map[string]openAPIProperty `json:"properties"`
}

type openAPIProperty struct {
	Const string `json:"const"`
}

func validateAnimaOpenAPI(document openAPIDocument) error {
	for _, requirement := range animaInvocationRequirements {
		schema, ok := document.Components.Schemas[requirement.schema]
		if !ok {
			return operation.UnsupportedCapability(fmt.Sprintf(
				"InvokeAI does not provide required invocation schema %s for %s", requirement.schema, requirement.typeName,
			))
		}
		if schema.Properties["type"].Const != requirement.typeName {
			return operation.UnsupportedCapability(fmt.Sprintf(
				"InvokeAI invocation schema %s does not identify type %s", requirement.schema, requirement.typeName,
			))
		}
		missing := make([]string, 0)
		for _, property := range requirement.properties {
			if _, ok := schema.Properties[property]; !ok {
				missing = append(missing, property)
			}
		}
		if len(missing) > 0 {
			return operation.UnsupportedCapability(fmt.Sprintf(
				"InvokeAI invocation schema %s for %s is missing required fields: %s",
				requirement.schema, requirement.typeName, strings.Join(missing, ", "),
			))
		}
	}
	return nil
}
