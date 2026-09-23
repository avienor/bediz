package upscale

import (
	"strings"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
)

// family holds everything that differs between the tested stock upscale
// branches. The rest of the graph, the bounds, and the receipt are shared.
type family struct {
	base        string
	label       string
	tileStarter string
	graphID     string
	entry       func() capability.Entry
	// conditioning adds the main loader and prompt conditioning nodes. Their
	// edges must feed the denoiser's unet and both conditioning inputs, and
	// the loader must be named model_loader so it can supply the bundled VAE.
	conditioning func(model graphops.ModelReference, nodes map[string]any) []graphops.Edge
}

var families = []family{
	{
		base: "sdxl", label: "SDXL", tileStarter: "xinsir/controlNet-tile-sdxl-1.0",
		graphID: "bediz_sdxl_upscale_v1", entry: capability.SDXLUpscaleEntry, conditioning: sdxlConditioning,
	},
	{
		base: "sd-1", label: "SD1.5", tileStarter: "lllyasviel/control_v11f1e_sd15_tile",
		graphID: "bediz_sd1_upscale_v1", entry: capability.SD1UpscaleEntry, conditioning: sd1Conditioning,
	},
}

// familyLabels names the supported families for caller-facing messages.
func familyLabels() string {
	labels := make([]string, len(families))
	for index, candidate := range families {
		labels[index] = candidate.label
	}
	return strings.Join(labels, " or ")
}

func familyFor(base string) (family, bool) {
	for _, candidate := range families {
		if candidate.base == base {
			return candidate, true
		}
	}
	return family{}, false
}

// sdxlConditioning is the stock SDXL branch, whose style inputs equal their prompts.
func sdxlConditioning(model graphops.ModelReference, nodes map[string]any) []graphops.Edge {
	nodes["model_loader"] = node("model_loader", "sdxl_model_loader", map[string]any{"model": model})
	nodes["positive_conditioning"] = node("positive_conditioning", "sdxl_compel_prompt", nil)
	nodes["negative_conditioning"] = node("negative_conditioning", "sdxl_compel_prompt", nil)
	return []graphops.Edge{
		graphops.Connect("model_loader", "clip", "positive_conditioning", "clip"),
		graphops.Connect("model_loader", "clip2", "positive_conditioning", "clip2"),
		graphops.Connect("model_loader", "clip", "negative_conditioning", "clip"),
		graphops.Connect("model_loader", "clip2", "negative_conditioning", "clip2"),
		graphops.Connect("model_loader", "unet", "denoise", "unet"),
		graphops.Connect("positive_prompt", "value", "positive_conditioning", "prompt"),
		graphops.Connect("positive_prompt", "value", "positive_conditioning", "style"),
		graphops.Connect("negative_prompt", "value", "negative_conditioning", "prompt"),
		graphops.Connect("negative_prompt", "value", "negative_conditioning", "style"),
	}
}

// sd1Conditioning is the stock SD1.5 branch. Stock omits skipped_layers and
// relies on its default of 0; Bediz sends the same value explicitly because
// clip skip is not a public setting.
func sd1Conditioning(model graphops.ModelReference, nodes map[string]any) []graphops.Edge {
	nodes["model_loader"] = node("model_loader", "main_model_loader", map[string]any{"model": model})
	nodes["clip_skip"] = node("clip_skip", "clip_skip", map[string]any{"skipped_layers": 0})
	nodes["positive_conditioning"] = node("positive_conditioning", "compel", nil)
	nodes["negative_conditioning"] = node("negative_conditioning", "compel", nil)
	return []graphops.Edge{
		graphops.Connect("model_loader", "clip", "clip_skip", "clip"),
		graphops.Connect("clip_skip", "clip", "positive_conditioning", "clip"),
		graphops.Connect("clip_skip", "clip", "negative_conditioning", "clip"),
		graphops.Connect("model_loader", "unet", "denoise", "unet"),
		graphops.Connect("positive_prompt", "value", "positive_conditioning", "prompt"),
		graphops.Connect("negative_prompt", "value", "negative_conditioning", "prompt"),
	}
}
