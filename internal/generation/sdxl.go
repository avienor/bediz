package generation

import (
	"fmt"
	"slices"
)

type sdxlModelLoaderNode struct {
	nodeAttributes
	Model modelReference `json:"model"`
}

type sdxlPromptNode struct {
	nodeAttributes
}

type sdxlNoiseNode struct {
	nodeAttributes
	Width  int  `json:"width"`
	Height int  `json:"height"`
	UseCPU bool `json:"use_cpu"`
}

type sdxlDenoiseNode struct {
	nodeAttributes
	CFGScale             float64 `json:"cfg_scale"`
	CFGRescaleMultiplier float64 `json:"cfg_rescale_multiplier"`
	DenoisingStart       float64 `json:"denoising_start"`
	DenoisingEnd         float64 `json:"denoising_end"`
	Steps                int     `json:"steps"`
	Scheduler            string  `json:"scheduler"`
}

type sdxlMetadataNode struct {
	nodeAttributes
	GenerationMode       string          `json:"generation_mode"`
	NegativePrompt       string          `json:"negative_prompt"`
	Width                int             `json:"width"`
	Height               int             `json:"height"`
	CFGScale             float64         `json:"cfg_scale"`
	CFGRescaleMultiplier float64         `json:"cfg_rescale_multiplier"`
	Steps                int             `json:"steps"`
	Scheduler            string          `json:"scheduler"`
	RandDevice           string          `json:"rand_device"`
	Model                modelReference  `json:"model"`
	VAE                  *modelReference `json:"vae,omitempty"`
}

type sdxlDecodeNode struct {
	nodeAttributes
	FP32  bool        `json:"fp32"`
	Board *boardField `json:"board,omitempty"`
}

type sdxlVAELoaderNode struct {
	nodeAttributes
	VAEModel modelReference `json:"vae_model"`
}

// CompileSDXL produces the stock InvokeAI 6.14.1 SDXL text-to-image graph.
func CompileSDXL(resolved Resolution) (EnqueueRequest, error) {
	request := resolved.Request
	if request.Width == nil || request.Height == nil || request.Steps == nil || request.Scheduler == nil || request.Guidance == nil || request.Seed == nil || request.OutputCount == nil {
		return EnqueueRequest{}, fmt.Errorf("compile SDXL graph: all generation settings must be resolved")
	}
	if len(resolved.Seeds) != *request.OutputCount || len(resolved.Seeds) == 0 || resolved.Seeds[0] != *request.Seed {
		return EnqueueRequest{}, fmt.Errorf("compile SDXL graph: resolved seeds do not match the request")
	}
	nodes := map[string]any{
		"model_loader":          sdxlModelLoaderNode{ID: "model_loader", IsIntermediate: true, UseCache: true, Type: "sdxl_model_loader", Model: reference(resolved.Models.Main)},
		"positive_prompt":       stringNode{ID: "positive_prompt", IsIntermediate: true, UseCache: true, Type: "string", Value: request.PositivePrompt},
		"negative_prompt":       stringNode{ID: "negative_prompt", IsIntermediate: true, UseCache: true, Type: "string", Value: request.NegativePrompt},
		"positive_conditioning": sdxlPromptNode{ID: "positive_conditioning", IsIntermediate: true, UseCache: true, Type: "sdxl_compel_prompt"},
		"negative_conditioning": sdxlPromptNode{ID: "negative_conditioning", IsIntermediate: true, UseCache: true, Type: "sdxl_compel_prompt"},
		"positive_collection":   collectNode{ID: "positive_collection", IsIntermediate: true, UseCache: true, Type: "collect", Collection: []any{}},
		"negative_collection":   collectNode{ID: "negative_collection", IsIntermediate: true, UseCache: true, Type: "collect", Collection: []any{}},
		"seed":                  integerNode{ID: "seed", IsIntermediate: true, UseCache: true, Type: "integer", Value: *request.Seed},
		"noise":                 sdxlNoiseNode{ID: "noise", IsIntermediate: true, UseCache: true, Type: "noise", Width: *request.Width, Height: *request.Height, UseCPU: true},
		"denoise":               sdxlDenoiseNode{ID: "denoise", IsIntermediate: true, UseCache: true, Type: "denoise_latents", CFGScale: *request.Guidance, CFGRescaleMultiplier: 0, DenoisingStart: 0, DenoisingEnd: 1, Steps: *request.Steps, Scheduler: *request.Scheduler},
	}
	metadata := sdxlMetadataNode{ID: "metadata", IsIntermediate: true, UseCache: true, Type: "core_metadata", GenerationMode: "sdxl_txt2img", NegativePrompt: request.NegativePrompt, Width: *request.Width, Height: *request.Height, CFGScale: *request.Guidance, CFGRescaleMultiplier: 0, Steps: *request.Steps, Scheduler: *request.Scheduler, RandDevice: "cpu", Model: reference(resolved.Models.Main)}
	decode := sdxlDecodeNode{ID: "decode", IsIntermediate: false, UseCache: false, Type: "l2i", FP32: true}
	if request.BoardID != "" {
		decode.Board = new(boardField{BoardID: request.BoardID})
	}
	edges := []Edge{
		edge("model_loader", "unet", "denoise", "unet"),
		edge("model_loader", "clip", "positive_conditioning", "clip"),
		edge("model_loader", "clip", "negative_conditioning", "clip"),
		edge("model_loader", "clip2", "positive_conditioning", "clip2"),
		edge("model_loader", "clip2", "negative_conditioning", "clip2"),
		edge("positive_prompt", "value", "positive_conditioning", "prompt"),
		edge("positive_prompt", "value", "positive_conditioning", "style"),
		edge("negative_prompt", "value", "negative_conditioning", "prompt"),
		edge("negative_prompt", "value", "negative_conditioning", "style"),
		edge("positive_conditioning", "conditioning", "positive_collection", "item"),
		edge("positive_collection", "collection", "denoise", "positive_conditioning"),
		edge("negative_conditioning", "conditioning", "negative_collection", "item"),
		edge("negative_collection", "collection", "denoise", "negative_conditioning"),
		edge("seed", "value", "noise", "seed"),
		edge("noise", "noise", "denoise", "noise"),
		edge("denoise", "latents", "decode", "latents"),
		edge("seed", "value", "metadata", "seed"),
		edge("positive_prompt", "value", "metadata", "positive_prompt"),
		edge("negative_prompt", "value", "metadata", "negative_prompt"),
		edge("metadata", "metadata", "decode", "metadata"),
	}
	vaeSource := "model_loader"
	if resolved.Models.VAE.Key != "" {
		vae := resolved.Models.VAE
		metadata.VAE = new(reference(vae))
		nodes["vae_loader"] = sdxlVAELoaderNode{ID: "vae_loader", IsIntermediate: true, UseCache: true, Type: "vae_loader", VAEModel: reference(vae)}
		vaeSource = "vae_loader"
	}
	nodes["metadata"] = metadata
	nodes["decode"] = decode
	edges = append(edges, edge(vaeSource, "vae", "decode", "vae"))
	batchSeeds := slices.Clone(resolved.Seeds)
	slices.Reverse(batchSeeds)
	return EnqueueRequest{Batch: Batch{Origin: "generate", Destination: "generate", Graph: Graph{ID: "bediz_sdxl_v1", Nodes: nodes, Edges: edges}, Data: [][]BatchDatum{{{NodePath: "seed", FieldName: "value", Items: batchSeeds}}}, Runs: 1}}, nil
}
