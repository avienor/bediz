# 04: Generate FLUX.1 images from a Source Image

**What to build:** `generate` performs image-to-image generation for FLUX.1 main models under the contract ticket 02 established. The feature spec (`.scratch/img2img/spec.md`) and the V1 spec image-to-image subsection hold the shared rules. FLUX.1-specific behavior:

- **Accepted models:** Exactly the text-to-image set: variant `dev` or `schnell` in format `checkpoint`, `bnb_quantized_nf4b`, or `gguf_quantized`.
  - Kontext dev and Krea dev report variant `dev` and are accepted by variant. Bediz does not replicate the stock frontend's display-name block on Kontext.
  - `dev_fill`, unknown variants, SDNQ, and ordinary `diffusers` stay `unsupported_capability`.
  - A non-empty negative prompt, or guidance for schnell, stays `invalid_request`.
- **Strength:** It follows InvokeAI 6.14.1's default "optimized denoising": `denoising_start = 1 − strength^0.2` and `denoising_end = 1`. For example, strength 0.75 gives about 0.0559 and strength 0.5 about 0.1294. The receipt reports the public `strength`, not `denoising_start`.
- **Graph:** The tested FLUX.1 text-to-image topology, plus:
  - `flux_vae_encode`, fed by the source image or by an intermediate `img_resize` (`resample_mode: "bicubic"`) when the resolved dimensions differ from the source;
  - the `flux_model_loader` VAE feeding both `flux_vae_encode` and `flux_vae_decode`;
  - the encoded latents into `flux_denoise.latents`, with `add_noise: true`;
  - `flux_denoise` width and height at the resolved dimensions;
  - `core_metadata` with `generation_mode: "flux_img2img"`, `strength`, and `init_image`.
- **Dimensions:** Unless an explicit pair is given, source dimensions are rounded down to a multiple of 16. A source smaller than 16 pixels in either dimension is `invalid_request` before any upload or enqueue.
- **UI Synchronization:** The Recall patch is the FLUX.1 text-to-image patch. `ui_sync_partial.not_restored` is the FLUX.1 list (`scheduler`, `guidance` for dev only, `vae`, `t5_encoder`, `clip_embed`, `output_count`, `board_id`) followed by `source_image` and `strength`.
- **Doctor:** A `generate`/`flux` entry with `mode: "img2img"` requires:
  - the FLUX.1 text-to-image requirements, including its variant- and format-filtered main-model requirement;
  - the image inspection and upload endpoints;
  - `ImageResizeInvocation` (`img_resize`) with `image`, `width`, `height`, and `resample_mode`;
  - `FluxVaeEncodeInvocation` (`flux_vae_encode`) with `image` and `vae`;
  - `FluxDenoiseInvocation` with its text-to-image properties plus `latents`, `denoising_start`, `denoising_end`, and `add_noise`;
  - `CoreMetadataInvocation` with its text-to-image properties plus `strength` and `init_image`.

  Each schema also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties. `generate` checks the same requirements before any upload or enqueue. The entry carries `ui_sync: partial` only when Recall is compatible. The text-to-image `generate/flux` entry does not gain the three denoising properties in this ticket; that pre-existing gap is out of scope.

**Blocked by:** 02: Generate SDXL images from a Source Image. It is independent of ticket 03 and may run in parallel with it.

**Execution route:** `worker + independent review`. The strength mapping and the Kontext policy were decided with the user on 2026-09-28. The remaining work is one family adapter whose correctness fixtures, computed values, and live runs can detect.

**Verification gate:**
- Versioned InvokeAI 6.14.1 FLUX.1 image-to-image enqueue fixtures exist for dev and schnell, with and without resize, and are checked against the live OpenAPI invocation vocabulary. They pin `denoising_start` for representative strengths, including 1, 0.75, and a small value.
- Public-seam tests prove:
  - 16-pixel dimension rounding, and a too-small source rejected before any upload or enqueue;
  - that each missing FLUX.1 image-to-image schema or property listed above fails `generate` as `unsupported_capability` before any upload, and marks the `doctor` entry incompatible with the property named, while the FLUX.1 text-to-image entry stays compatible;
  - `dev_fill`, unknown variant, and SDNQ rejection before any upload;
  - that Kontext-named dev models are accepted by variant;
  - that negative prompts and schnell guidance are still rejected;
  - the FLUX.1 `not_restored` list for dev and schnell;
  - the `doctor` FLUX.1 image-to-image entry's matching and failures, including an inventory with only `dev_fill` or SDNQ main models;
  - that FLUX.1 text-to-image fixtures and receipts are unchanged.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live checks against the local InvokeAI 6.14.1 baseline with the installed FLUX.1 dev and schnell NF4 models and their components (no installation needed), following the live-verification guide:
  - One waited 768 × 768 schnell image-to-image and one waited dev image-to-image produce receipts whose images carry the matching seeds and `flux_img2img` metadata.
  - `doctor --json` reports the entry as compatible.
  - The live E2E gate (`BEDIZ_E2E_URL` set) passes with the entry registered and covers it as the guide requires.
- Report unavailable live steps, including 8 GB VRAM limits, and list ad hoc images.

**Review gate:**
- A separate reviewer reviews a fixed base/head diff, starting from this ticket, the feature spec, the V1 spec image-to-image subsection, and the worker's evidence. The reviewer must be able to read the InvokeAI 6.14.1 invocation schemas and the built frontend bundle.
- Before any fix, the reviewer:
  - recomputes `denoising_start` for the pinned strengths from the frontend's formula;
  - compares the fixtures against the stock frontend's FLUX image-to-image branch;
  - independently reruns one live schnell image-to-image;
  - checks the public tests against the spec.
- Blocking findings: a formula or rounding mismatch, any display-name-based model inference, acceptance of `dev_fill` or another unsupported variant or format, a changed text-to-image fixture or receipt, or unreproducible live evidence.

**Escalate when:**
- The Go and frontend computations of `strength^0.2` disagree for a pinned value.
- `flux_vae_encode` or `flux_denoise` needs inputs the spec does not record.
- A Kontext or Krea model fails image-to-image in a way that suggests a product decision.
- The baseline hardware cannot complete a generation and the failure looks like a graph defect rather than a resource limit.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec: the image-to-image subsection's FLUX.1 row with the strength formula and the Kontext rule, the §12 FLUX.1 `not_restored` list, and the §10 FLUX.1 image-to-image entry.
- `CHANGELOG.md` under Unreleased. Tests.
- No ADR or glossary change: ADR-0023 records the strength meaning.

**Status:** ready-for-agent

- [ ] `generate` with a supported FLUX.1 dev or schnell main model and a Source Image produces image-to-image outputs through one tested enqueue, using the recorded strength mapping.
- [ ] Unsupported variants and formats fail before any upload, and no model is classified by display name.
- [ ] Doctor, `ui_sync_partial`, the V1 spec, and `CHANGELOG.md` reflect the behavior verified live.
- [ ] The independent review found no acceptance-blocking issue.
