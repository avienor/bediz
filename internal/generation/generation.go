package generation

import (
	"fmt"
	"slices"
)

type Components struct {
	VAE          *string `json:"vae,omitempty"`
	Qwen3Encoder *string `json:"qwen3_encoder,omitempty"`
	T5Encoder    *string `json:"t5_encoder,omitempty"`
	CLIPEmbed    *string `json:"clip_embed,omitempty"`
}

type Request struct {
	SchemaVersion  int         `json:"schema_version"`
	Model          string      `json:"model"`
	PositivePrompt string      `json:"positive_prompt"`
	NegativePrompt string      `json:"negative_prompt,omitempty"`
	Width          *int        `json:"width,omitempty"`
	Height         *int        `json:"height,omitempty"`
	Steps          *int        `json:"steps,omitempty"`
	Scheduler      *string     `json:"scheduler,omitempty"`
	Guidance       *float64    `json:"guidance,omitempty"`
	Seed           *uint32     `json:"seed,omitempty"`
	OutputCount    *int        `json:"output_count,omitempty"`
	BoardID        string      `json:"board_id,omitempty"`
	Components     *Components `json:"components,omitempty"`
}

type ModelIdentifier struct {
	Key     string `json:"key"`
	Hash    string `json:"hash"`
	Name    string `json:"name"`
	Base    string `json:"base"`
	Type    string `json:"type"`
	Variant string `json:"variant,omitempty"`
	Format  string `json:"format,omitempty"`
}

type ResolvedModels struct {
	Main         ModelIdentifier
	VAE          ModelIdentifier
	Qwen3Encoder ModelIdentifier
	T5Encoder    ModelIdentifier
	CLIPEmbed    ModelIdentifier
}

type EdgeConnection struct {
	NodeID string `json:"node_id"`
	Field  string `json:"field"`
}

type Edge struct {
	Source      EdgeConnection `json:"source"`
	Destination EdgeConnection `json:"destination"`
}

type Graph struct {
	ID    string         `json:"id"`
	Nodes map[string]any `json:"nodes"`
	Edges []Edge         `json:"edges"`
}

type nodeAttributes struct {
	ID             string `json:"id"`
	IsIntermediate bool   `json:"is_intermediate"`
	UseCache       bool   `json:"use_cache"`
	Type           string `json:"type"`
}

type animaModelLoaderNode struct {
	nodeAttributes
	Model             modelReference `json:"model"`
	VAEModel          modelReference `json:"vae_model"`
	Qwen3EncoderModel modelReference `json:"qwen3_encoder_model"`
}

type stringNode struct {
	nodeAttributes
	Value string `json:"value"`
}

type connectedAnimaTextEncoderNode struct {
	nodeAttributes
}

type promptAnimaTextEncoderNode struct {
	nodeAttributes
	Prompt string `json:"prompt"`
}

type collectNode struct {
	nodeAttributes
	Collection []any `json:"collection"`
}

type integerNode struct {
	nodeAttributes
	Value uint32 `json:"value"`
}

type animaDenoiseNode struct {
	nodeAttributes
	DenoisingStart float64 `json:"denoising_start"`
	DenoisingEnd   float64 `json:"denoising_end"`
	AddNoise       bool    `json:"add_noise"`
	GuidanceScale  float64 `json:"guidance_scale"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Steps          int     `json:"steps"`
	Seed           uint32  `json:"seed"`
	Scheduler      string  `json:"scheduler"`
}

type coreMetadataNode struct {
	nodeAttributes
	GenerationMode string         `json:"generation_mode"`
	NegativePrompt string         `json:"negative_prompt"`
	Width          int            `json:"width"`
	Height         int            `json:"height"`
	CFGScale       float64        `json:"cfg_scale"`
	Steps          int            `json:"steps"`
	Scheduler      string         `json:"scheduler"`
	Model          modelReference `json:"model"`
	VAE            modelReference `json:"vae"`
	Qwen3Encoder   modelReference `json:"qwen3_encoder"`
}

type boardField struct {
	BoardID string `json:"board_id"`
}

type animaLatentsToImageNode struct {
	nodeAttributes
	Board *boardField `json:"board,omitempty"`
}

type Batch struct {
	Origin      string         `json:"origin"`
	Destination string         `json:"destination"`
	Graph       Graph          `json:"graph"`
	Data        [][]BatchDatum `json:"data"`
	Runs        int            `json:"runs"`
}

type BatchDatum struct {
	NodePath  string   `json:"node_path"`
	FieldName string   `json:"field_name"`
	Items     []uint32 `json:"items"`
}

type EnqueueRequest struct {
	Batch Batch `json:"batch"`
}

func CompileAnima(resolved AnimaResolution) (EnqueueRequest, error) {
	request := resolved.Request
	if request.Width == nil || request.Height == nil || request.Steps == nil || request.Scheduler == nil ||
		request.Guidance == nil || request.Seed == nil || request.OutputCount == nil {
		return EnqueueRequest{}, fmt.Errorf("compile Anima graph: all generation settings must be resolved")
	}
	if len(resolved.Seeds) != *request.OutputCount || len(resolved.Seeds) == 0 || resolved.Seeds[0] != *request.Seed {
		return EnqueueRequest{}, fmt.Errorf("compile Anima graph: resolved seeds do not match the request")
	}

	nodes := map[string]any{
		"model_loader": animaModelLoaderNode{
			ID: "model_loader", IsIntermediate: true, UseCache: true, Type: "anima_model_loader",
			Model: reference(resolved.Models.Main), VAEModel: reference(resolved.Models.VAE), Qwen3EncoderModel: reference(resolved.Models.Qwen3Encoder),
		},
		"positive_prompt": stringNode{
			ID: "positive_prompt", IsIntermediate: true, UseCache: true, Type: "string",
			Value: request.PositivePrompt,
		},
		"positive_conditioning": connectedAnimaTextEncoderNode{
			ID: "positive_conditioning", IsIntermediate: true, UseCache: true, Type: "anima_text_encoder",
		},
		"positive_collection": collectNode{
			ID: "positive_collection", IsIntermediate: true, UseCache: true, Type: "collect",
			Collection: []any{},
		},
		"negative_conditioning": promptAnimaTextEncoderNode{
			ID: "negative_conditioning", IsIntermediate: true, UseCache: true, Type: "anima_text_encoder",
			Prompt: request.NegativePrompt,
		},
		"negative_collection": collectNode{
			ID: "negative_collection", IsIntermediate: true, UseCache: true, Type: "collect",
			Collection: []any{},
		},
		"seed": integerNode{
			ID: "seed", IsIntermediate: true, UseCache: true, Type: "integer",
			Value: *request.Seed,
		},
		"denoise": animaDenoiseNode{
			ID: "denoise", IsIntermediate: true, UseCache: true, Type: "anima_denoise",
			DenoisingStart: 0, DenoisingEnd: 1, AddNoise: true,
			GuidanceScale: *request.Guidance, Width: *request.Width, Height: *request.Height,
			Steps: *request.Steps, Seed: 0, Scheduler: *request.Scheduler,
		},
		"metadata": coreMetadataNode{
			ID: "metadata", IsIntermediate: true, UseCache: true, Type: "core_metadata",
			GenerationMode: "anima_txt2img", NegativePrompt: request.NegativePrompt,
			Width: *request.Width, Height: *request.Height, CFGScale: *request.Guidance,
			Steps: *request.Steps, Scheduler: *request.Scheduler,
			Model: reference(resolved.Models.Main), VAE: reference(resolved.Models.VAE), Qwen3Encoder: reference(resolved.Models.Qwen3Encoder),
		},
		"decode": animaLatentsToImageNode{
			ID: "decode", IsIntermediate: false, UseCache: false, Type: "anima_l2i",
		},
	}
	if request.BoardID != "" {
		nodes["decode"] = animaLatentsToImageNode{
			ID: "decode", IsIntermediate: false, UseCache: false, Type: "anima_l2i",
			Board: new(boardField{BoardID: request.BoardID}),
		}
	}

	edges := []Edge{
		edge("model_loader", "transformer", "denoise", "transformer"),
		edge("model_loader", "qwen3_encoder", "positive_conditioning", "qwen3_encoder"),
		edge("model_loader", "vae", "decode", "vae"),
		edge("positive_prompt", "value", "positive_conditioning", "prompt"),
		edge("positive_conditioning", "conditioning", "positive_collection", "item"),
		edge("positive_collection", "collection", "denoise", "positive_conditioning"),
		edge("model_loader", "qwen3_encoder", "negative_conditioning", "qwen3_encoder"),
		edge("negative_conditioning", "conditioning", "negative_collection", "item"),
		edge("negative_collection", "collection", "denoise", "negative_conditioning"),
		edge("seed", "value", "denoise", "seed"),
		edge("denoise", "latents", "decode", "latents"),
		edge("seed", "value", "metadata", "seed"),
		edge("positive_prompt", "value", "metadata", "positive_prompt"),
		edge("metadata", "metadata", "decode", "metadata"),
	}

	batchSeeds := slices.Clone(resolved.Seeds)
	// InvokeAI 6.14 returns enqueue item IDs newest-first while expanding batch
	// values in listed order. Reverse the adapter payload so each returned item
	// ID has the same-position seed in the resolved receipt.
	slices.Reverse(batchSeeds)
	return EnqueueRequest{Batch: Batch{
		Origin: "generate", Destination: "generate",
		Graph: Graph{ID: "bediz_anima_v1", Nodes: nodes, Edges: edges},
		Data:  [][]BatchDatum{{{NodePath: "seed", FieldName: "value", Items: batchSeeds}}},
		Runs:  1,
	}}, nil
}

func edge(sourceNode, sourceField, destinationNode, destinationField string) Edge {
	return Edge{
		Source:      EdgeConnection{NodeID: sourceNode, Field: sourceField},
		Destination: EdgeConnection{NodeID: destinationNode, Field: destinationField},
	}
}
