# 03: Generate Anima images from a Source Image

**What to build:** `generate` performs image-to-image generation for Anima main models under the contract ticket 02 established. The feature spec (`.scratch/img2img/spec.md`) and the V1 spec image-to-image subsection hold the shared rules. Anima-specific behavior:

- **Graph:** The tested Anima text-to-image topology, including its negative conditioning branch, plus:
  - `anima_i2l`, fed by the source image or by an intermediate `img_resize` (`resample_mode: "bicubic"`) when the resolved dimensions differ from the source;
  - the `anima_model_loader` VAE feeding both `anima_i2l` and `anima_l2i`;
  - `anima_i2l` latents into `anima_denoise.latents`, with `denoising_start = 1 − strength`, `denoising_end = 1`, and `add_noise: true`;
  - `anima_denoise` width and height at the resolved dimensions;
  - `core_metadata` with `generation_mode: "anima_img2img"`, `strength`, and `init_image`.
- **Dimensions:** Unless an explicit pair is given, source dimensions are rounded down to a multiple of 8. A source smaller than 8 pixels in either dimension is `invalid_request` before any upload or enqueue.
- **UI Synchronization:** The Recall patch is the Anima text-to-image patch. `ui_sync_partial.not_restored` is `scheduler`, `guidance`, `vae`, `qwen3_encoder`, `output_count`, `board_id`, `source_image`, `strength`.
- **Doctor:** A `generate`/`anima` entry with `mode: "img2img"` requires:
  - the Anima text-to-image requirements;
  - the image inspection and upload endpoints;
  - `ImageResizeInvocation` (`img_resize`) with `image`, `width`, `height`, and `resample_mode`;
  - `AnimaImageToLatentsInvocation` (`anima_i2l`) with `image` and `vae`;
  - `AnimaDenoiseInvocation` with its text-to-image properties plus `latents`;
  - `CoreMetadataInvocation` with its text-to-image properties plus `strength` and `init_image`.

  Each schema also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties. `generate` checks the same requirements before any upload or enqueue. The entry carries `ui_sync: partial` only when Recall is compatible.
- **Unsupported families:** An Anima source is no longer `unsupported_capability`. A FLUX.1 source stays unsupported unless ticket 04 has landed.

**Blocked by:** 02: Generate SDXL images from a Source Image.

**Execution route:** `worker + independent review`. Ticket 02 fixes the public contract. This slice adds one family adapter whose correctness fixtures, public tests, and a live run can detect.

**Verification gate:**
- A versioned InvokeAI 6.14.1 Anima image-to-image enqueue fixture exists, with and without resize, and is checked against the live OpenAPI invocation vocabulary.
- Public-seam tests prove:
  - Anima dimension rounding, and a too-small source rejected before any upload or enqueue;
  - that each missing Anima image-to-image schema or property listed above fails `generate` as `unsupported_capability` before any upload, and marks the `doctor` entry incompatible with the property named, while the Anima text-to-image entry stays compatible;
  - `denoising_start` for representative strengths;
  - component resolution unchanged from text-to-image;
  - the Anima `not_restored` list;
  - the `doctor` Anima image-to-image entry's matching and failures;
  - that Anima text-to-image fixtures and receipts are unchanged.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live checks against the local InvokeAI 6.14.1 baseline with `Anima Base 1.0` (no installation needed), following the live-verification guide:
  - A waited 768 × 768 image-to-image produces an output that visibly derives from its source, with metadata showing `anima_img2img`, `strength`, and `init_image`.
  - `doctor --json` reports the entry as compatible.
  - The live E2E gate (`BEDIZ_E2E_URL` set) passes with the entry registered and covers it as the guide requires.
- Report unavailable live steps and list ad hoc images.

**Review gate:**
- A separate reviewer reviews a fixed base/head diff, starting from this ticket, the feature spec, the V1 spec image-to-image subsection, and the worker's evidence. The reviewer must be able to read the InvokeAI 6.14.1 invocation schemas and the built frontend bundle.
- Before any fix, the reviewer:
  - compares the fixture against the stock frontend's Anima image-to-image branch and the backend `anima_i2l` and `anima_denoise` schemas;
  - independently reruns one live Anima image-to-image and checks its dimensions, seed, and metadata;
  - checks the public tests against the spec.
- Blocking findings: a graph difference the spec does not record, a contract drift from ticket 02, a changed text-to-image fixture or receipt, a missing rejection or bound test, or unreproducible live evidence.

**Escalate when:**
- `anima_i2l` rejects the installed Anima VAE, or needs inputs the spec does not record.
- Outputs show no influence from the source.
- The stock graph requires a conditioning shape different from Bediz's tested text-to-image shape.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec: the image-to-image subsection's Anima row, the §12 Anima `not_restored` list, and the §10 Anima image-to-image entry.
- `CHANGELOG.md` under Unreleased. Tests.
- No ADR or glossary change: ADR-0023 and the terms from ticket 02 cover this slice.

**Status:** ready-for-agent

- [ ] `generate` with an Anima main model and a Source Image produces image-to-image outputs through one tested enqueue, as recorded.
- [ ] Doctor, `ui_sync_partial`, the V1 spec, and `CHANGELOG.md` reflect the behavior verified live.
- [ ] The independent review found no acceptance-blocking issue.
