---
status: accepted
---

# Target InvokeAI's generative upscale flow

The V1 `upscale` operation will implement InvokeAI's tiled multi-diffusion upscale behavior rather than a standalone RealESRGAN invocation. Initial support will cover the SD1.5 and SDXL model families, matching the supported families in InvokeAI's current upscale graph. The operation will resolve a compatible main model, Spandrel image-to-image upscale model, Tile ControlNet, and optional VAE under the same unambiguous component-resolution rules as generation.

An upscale source may be an existing InvokeAI image name or an absolute local image path. Bediz will upload a local source before enqueueing the upscale graph and will record the resulting image reference in the execution receipt. If a later step fails, the successfully uploaded source remains in InvokeAI and will be reported rather than deleted automatically.

The typed V1 request may include positive and negative prompts, scale, creativity, structure, steps, scheduler, CFG scale, seed, tile size, tile overlap, optional VAE, and output board. LoRAs and additional post-processing models are deferred.

On stock InvokeAI 6.14.x, the operation has partial UI synchronization: the queue item, result, metadata, prompts and seed where supported, and execution receipt are visible, but Recall cannot restore every Upscale-panel field.

## Consequences

The command matches the creative upscale panel users recognize in InvokeAI and can add generated detail rather than merely resizing pixels. It requires more graph and component validation than basic super-resolution, consumes substantially more compute, and is limited to SD1.5 and SDXL until InvokeAI and Bediz define tested flows for other families.
