package generation

import (
	"fmt"
	"slices"
)

// modelReference includes only the fields accepted by InvokeAI's model identifier input.
type modelReference struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
	Name string `json:"name"`
	Base string `json:"base"`
	Type string `json:"type"`
}

func reference(model ModelIdentifier) modelReference {
	return modelReference{Key: model.Key, Hash: model.Hash, Name: model.Name, Base: model.Base, Type: model.Type}
}

type fluxLoaderNode struct {
	nodeAttributes
	Model          modelReference `json:"model"`
	VAEModel       modelReference `json:"vae_model"`
	T5EncoderModel modelReference `json:"t5_encoder_model"`
	CLIPEmbedModel modelReference `json:"clip_embed_model"`
}

type fluxDenoiseNode struct {
	nodeAttributes
	DenoisingStart float64  `json:"denoising_start"`
	DenoisingEnd   float64  `json:"denoising_end"`
	AddNoise       bool     `json:"add_noise"`
	CFGScale       float64  `json:"cfg_scale"`
	Width          int      `json:"width"`
	Height         int      `json:"height"`
	NumSteps       int      `json:"num_steps"`
	Seed           uint32   `json:"seed"`
	Scheduler      string   `json:"scheduler"`
	Guidance       *float64 `json:"guidance,omitempty"`
}

type fluxMetadataNode struct {
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
}

type fluxDecodeNode struct {
	nodeAttributes
	Board *boardField `json:"board,omitempty"`
}

// CompileFLUX produces the stock InvokeAI 6.14.1 FLUX.1 text-to-image graph.
func CompileFLUX(resolved Resolution) (EnqueueRequest, error) {
	r := resolved.Request
	if r.Width == nil || r.Height == nil || r.Steps == nil || r.Scheduler == nil || r.Seed == nil || r.OutputCount == nil {
		return EnqueueRequest{}, fmt.Errorf("compile FLUX.1 graph: all generation settings must be resolved")
	}
	if err := validateFLUXMain(resolved.Models.Main); err != nil {
		return EnqueueRequest{}, err
	}
	if resolved.Models.Main.Variant == "dev" && r.Guidance == nil || resolved.Models.Main.Variant == "schnell" && r.Guidance != nil {
		return EnqueueRequest{}, fmt.Errorf("compile FLUX.1 graph: guidance does not match model variant")
	}
	if len(resolved.Seeds) != *r.OutputCount || len(resolved.Seeds) == 0 || resolved.Seeds[0] != *r.Seed {
		return EnqueueRequest{}, fmt.Errorf("compile FLUX.1 graph: resolved seeds do not match the request")
	}
	nodes := map[string]any{
		"model_loader":          fluxLoaderNode{ID: "model_loader", IsIntermediate: true, UseCache: true, Type: "flux_model_loader", Model: reference(resolved.Models.Main), VAEModel: reference(resolved.Models.VAE), T5EncoderModel: reference(resolved.Models.T5Encoder), CLIPEmbedModel: reference(resolved.Models.CLIPEmbed)},
		"positive_prompt":       stringNode{ID: "positive_prompt", IsIntermediate: true, UseCache: true, Type: "string", Value: r.PositivePrompt},
		"positive_conditioning": nodeAttributes{ID: "positive_conditioning", IsIntermediate: true, UseCache: true, Type: "flux_text_encoder"},
		"seed":                  integerNode{ID: "seed", IsIntermediate: true, UseCache: true, Type: "integer", Value: *r.Seed},
		"denoise":               fluxDenoiseNode{nodeAttributes: nodeAttributes{ID: "denoise", IsIntermediate: true, UseCache: true, Type: "flux_denoise"}, DenoisingStart: 0, DenoisingEnd: 1, AddNoise: true, CFGScale: 1, Width: *r.Width, Height: *r.Height, NumSteps: *r.Steps, Seed: 0, Scheduler: *r.Scheduler, Guidance: r.Guidance},
		"metadata":              fluxMetadataNode{nodeAttributes: nodeAttributes{ID: "metadata", IsIntermediate: true, UseCache: true, Type: "core_metadata"}, GenerationMode: "flux_txt2img", NegativePrompt: "", Width: *r.Width, Height: *r.Height, CFGScale: 1, Steps: *r.Steps, Scheduler: *r.Scheduler, Model: reference(resolved.Models.Main), VAE: reference(resolved.Models.VAE)},
		"decode":                fluxDecodeNode{nodeAttributes: nodeAttributes{ID: "decode", IsIntermediate: false, UseCache: false, Type: "flux_vae_decode"}},
	}
	if r.BoardID != "" {
		nodes["decode"] = fluxDecodeNode{nodeAttributes: nodeAttributes{ID: "decode", IsIntermediate: false, UseCache: false, Type: "flux_vae_decode"}, Board: new(boardField{BoardID: r.BoardID})}
	}
	edges := []Edge{
		edge("model_loader", "transformer", "denoise", "transformer"),
		edge("model_loader", "clip", "positive_conditioning", "clip"),
		edge("model_loader", "t5_encoder", "positive_conditioning", "t5_encoder"),
		edge("model_loader", "max_seq_len", "positive_conditioning", "t5_max_seq_len"),
		edge("positive_prompt", "value", "positive_conditioning", "prompt"),
		edge("positive_conditioning", "conditioning", "denoise", "positive_text_conditioning"),
		edge("seed", "value", "denoise", "seed"),
		edge("denoise", "latents", "decode", "latents"),
		edge("model_loader", "vae", "decode", "vae"),
		edge("seed", "value", "metadata", "seed"),
		edge("positive_prompt", "value", "metadata", "positive_prompt"),
		edge("metadata", "metadata", "decode", "metadata"),
	}
	seeds := slices.Clone(resolved.Seeds)
	slices.Reverse(seeds)
	return EnqueueRequest{Batch: Batch{Origin: "generate", Destination: "generate", Graph: Graph{ID: "bediz_flux_v1", Nodes: nodes, Edges: edges}, Data: [][]BatchDatum{{{NodePath: "seed", FieldName: "value", Items: seeds}}}, Runs: 1}}, nil
}
