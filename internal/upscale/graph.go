package upscale

import (
	"fmt"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/images"
)

// CompileSDXL creates the tested InvokeAI 6.14.1 tiled SDXL upscale graph.
func CompileSDXL(resolved Resolution, source images.Reference) (graphops.EnqueueRequest, error) {
	r := resolved.Request
	if r.Scale == nil || r.Creativity == nil || r.Structure == nil || r.Steps == nil || r.Scheduler == nil ||
		r.Guidance == nil || r.Seed == nil || r.TileSize == nil || r.TileOverlap == nil ||
		resolved.Models.Main.Key == "" || resolved.Models.UpscaleModel.Key == "" || resolved.Models.TileControlNet.Key == "" || source.ImageName == "" {
		return graphops.EnqueueRequest{}, fmt.Errorf("compile SDXL upscale graph: request, models, and source must be resolved")
	}
	if *r.Seed != resolved.Seed {
		return graphops.EnqueueRequest{}, fmt.Errorf("compile SDXL upscale graph: seed does not match resolution")
	}
	model := graphops.Reference(resolved.Models.Main)
	upscaleModel := graphops.Reference(resolved.Models.UpscaleModel)
	controlModel := graphops.Reference(resolved.Models.TileControlNet)
	imageField := map[string]any{"image_name": source.ImageName, "width": source.Width, "height": source.Height}
	denoisingStart := float64(10-*r.Creativity) * 4.99 / 100
	structure := float64(*r.Structure + 10)
	firstWeight := structure*0.0325 + 0.3
	firstEnd := structure*0.025 + 0.3
	secondWeight := (structure*0.0325 + 0.15) * 0.45

	nodes := map[string]any{
		"positive_prompt": node("positive_prompt", "string", nil),
		"negative_prompt": node("negative_prompt", "string", nil),
		"seed":            node("seed", "integer", nil),
		"autoscale": node("autoscale", "spandrel_image_to_image_autoscale", map[string]any{
			"image": imageField, "image_to_image_model": upscaleModel, "scale": *r.Scale, "fit_to_multiple_of_8": true,
		}),
		"unsharp_mask": node("unsharp_mask", "unsharp_mask", map[string]any{"radius": 2, "strength": 60}),
		"noise":        node("noise", "noise", nil),
		"encode":       node("encode", "i2l", map[string]any{"tiled": true, "tile_size": *r.TileSize, "fp32": true}),
		"decode":       node("decode", "l2i", map[string]any{"tiled": true, "tile_size": *r.TileSize, "fp32": true}),
		"denoise": node("denoise", "tiled_multi_diffusion_denoise_latents", map[string]any{
			"tile_width": *r.TileSize, "tile_height": *r.TileSize, "tile_overlap": *r.TileOverlap,
			"steps": *r.Steps, "cfg_scale": *r.Guidance, "scheduler": *r.Scheduler,
			"denoising_start": denoisingStart, "denoising_end": 1,
		}),
		"model_loader":          node("model_loader", "sdxl_model_loader", map[string]any{"model": model}),
		"positive_conditioning": node("positive_conditioning", "sdxl_compel_prompt", nil),
		"negative_conditioning": node("negative_conditioning", "sdxl_compel_prompt", nil),
		"controlnet_1": node("controlnet_1", "controlnet", map[string]any{
			"control_model": controlModel, "control_weight": firstWeight, "begin_step_percent": 0,
			"end_step_percent": firstEnd, "control_mode": "balanced", "resize_mode": "just_resize",
		}),
		"controlnet_2": node("controlnet_2", "controlnet", map[string]any{
			"control_model": controlModel, "control_weight": secondWeight, "begin_step_percent": firstEnd,
			"end_step_percent": 0.85, "control_mode": "balanced", "resize_mode": "just_resize",
		}),
		"control_collection": node("control_collection", "collect", nil),
	}
	decode := nodes["decode"].(map[string]any)
	decode["is_intermediate"] = false
	if r.BoardID != "" {
		decode["board"] = graphops.BoardField{BoardID: r.BoardID}
	}
	metadata := node("metadata", "core_metadata", map[string]any{
		"steps": *r.Steps, "scheduler": *r.Scheduler, "cfg_scale": *r.Guidance,
		"model": model, "upscale_model": upscaleModel, "creativity": *r.Creativity,
		"structure": *r.Structure, "tile_size": *r.TileSize, "tile_overlap": *r.TileOverlap,
		"upscale_initial_image": map[string]any{"image_name": source.ImageName, "width": source.Width, "height": source.Height},
		"upscale_scale":         *r.Scale,
	})
	nodes["metadata"] = metadata

	edges := []graphops.Edge{
		graphops.Connect("autoscale", "image", "unsharp_mask", "image"),
		graphops.Connect("unsharp_mask", "width", "noise", "width"),
		graphops.Connect("unsharp_mask", "height", "noise", "height"),
		graphops.Connect("seed", "value", "noise", "seed"),
		graphops.Connect("unsharp_mask", "image", "encode", "image"),
		graphops.Connect("unsharp_mask", "image", "controlnet_1", "image"),
		graphops.Connect("unsharp_mask", "image", "controlnet_2", "image"),
		graphops.Connect("model_loader", "clip", "positive_conditioning", "clip"),
		graphops.Connect("model_loader", "clip2", "positive_conditioning", "clip2"),
		graphops.Connect("model_loader", "clip", "negative_conditioning", "clip"),
		graphops.Connect("model_loader", "clip2", "negative_conditioning", "clip2"),
		graphops.Connect("model_loader", "unet", "denoise", "unet"),
		graphops.Connect("positive_prompt", "value", "positive_conditioning", "prompt"),
		graphops.Connect("positive_prompt", "value", "positive_conditioning", "style"),
		graphops.Connect("negative_prompt", "value", "negative_conditioning", "prompt"),
		graphops.Connect("negative_prompt", "value", "negative_conditioning", "style"),
		graphops.Connect("noise", "noise", "denoise", "noise"),
		graphops.Connect("encode", "latents", "denoise", "latents"),
		graphops.Connect("positive_conditioning", "conditioning", "denoise", "positive_conditioning"),
		graphops.Connect("negative_conditioning", "conditioning", "denoise", "negative_conditioning"),
		graphops.Connect("denoise", "latents", "decode", "latents"),
		graphops.Connect("controlnet_1", "control", "control_collection", "item"),
		graphops.Connect("controlnet_2", "control", "control_collection", "item"),
		graphops.Connect("control_collection", "collection", "denoise", "control"),
		graphops.Connect("positive_prompt", "value", "metadata", "positive_prompt"),
		graphops.Connect("negative_prompt", "value", "metadata", "negative_prompt"),
		graphops.Connect("seed", "value", "metadata", "seed"),
		graphops.Connect("autoscale", "width", "metadata", "width"),
		graphops.Connect("autoscale", "height", "metadata", "height"),
		graphops.Connect("metadata", "metadata", "decode", "metadata"),
	}
	vaeSource := "model_loader"
	if resolved.Models.VAE.Key != "" {
		vae := graphops.Reference(resolved.Models.VAE)
		metadata["vae"] = vae
		nodes["vae_loader"] = node("vae_loader", "vae_loader", map[string]any{"vae_model": vae})
		vaeSource = "vae_loader"
	}
	edges = append(edges,
		graphops.Connect(vaeSource, "vae", "encode", "vae"),
		graphops.Connect(vaeSource, "vae", "decode", "vae"),
	)
	return graphops.EnqueueRequest{Batch: graphops.Batch{
		Origin: "upscaling", Destination: "gallery",
		Graph: graphops.Graph{ID: "bediz_sdxl_upscale_v1", Nodes: nodes, Edges: edges},
		Data: [][]graphops.BatchDatum{
			{{NodePath: "seed", FieldName: "value", Items: []uint32{resolved.Seed}}},
			{
				{NodePath: "positive_prompt", FieldName: "value", StringItems: []string{r.PositivePrompt}},
				{NodePath: "negative_prompt", FieldName: "value", StringItems: []string{r.NegativePrompt}},
			},
		}, Runs: 1,
	}}, nil
}

func node(id, kind string, fields map[string]any) map[string]any {
	value := map[string]any{"id": id, "type": kind, "is_intermediate": true, "use_cache": true}
	for key, field := range fields {
		value[key] = field
	}
	return value
}
