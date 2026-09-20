---
status: accepted
---

# Target InvokeAI 6.14 and deliver V1 in vertical slices

The first Bediz release will target InvokeAI versions greater than or equal to 6.14.1 and lower than 6.15.0. Development and integration testing will use 6.14.1 as the baseline. Later 6.14 patch releases must still pass live compatibility checks, while a new minor or major InvokeAI version requires an explicit capability-matrix update.

Implementation will proceed through working vertical slices: the Go foundation and diagnostics; safe model, image, and queue inspection; end-to-end Anima generation; UI synchronization; model installation and source resolution; SDXL and FLUX.1 generation; SD1.5 and SDXL generative upscale; boards and profiles; and finally the canonical agent skill and release packaging.

V1 is complete when the supported generation and upscale families, management commands, Recall behavior, Hugging Face and Civitai source handling, stable JSON contract, capability matrix, canonical skill, and Linux amd64, Windows amd64, and macOS arm64 release artifacts are tested and documented. Canvas editing, brush and layer control, arbitrary workflow authoring, LoRAs, reference images, ControlNet generation, img2img, and inpainting remain outside V1.

## Consequences

Bediz reaches a usable end-to-end generation path before broadening its command surface. InvokeAI 6.15 and 7.x will not be treated as compatible merely because their endpoints look similar. Deferred creative features can be designed against a stable controller contract after the first release.
