# 02: Generate SDXL images from a Source Image

**What to build:** `generate` performs image-to-image generation for SDXL main models when the request names a Source Image. This ticket establishes the public image-to-image contract that tickets 03 and 04 extend to Anima and FLUX.1. The feature spec (`.scratch/img2img/spec.md`) records every decision; the essentials follow.

- **Request:** The Generation Request gains two optional members.
  - `source`: the upscale shape, `{"type":"image","reference":...}` or `{"type":"path","reference":"<absolute local path>"}`. Flags `--image` and `--image-path`; supplying both is `invalid_request`.
  - `strength`: flag `--strength`. It must be finite with `0 < strength ≤ 1`, and defaults to 0.75.

  `strength` without `source` is `invalid_request`. Every `strength` error names `strength` in `error.details.field`. Source validation errors keep the upscale behavior. A request without `source` behaves exactly as today.
- **Order:**
  1. Every local check runs before any network request. For a `path` source this includes the `images upload` image-content check and reading the width and height from the PNG, JPEG, or GIF header with the Go standard library. A WebP, BMP, or ICO `path` source is `invalid_request` at this step, and its message points to `images upload` followed by an `image` source. Upscale `path` sources are unaffected.
  2. After main-model resolution comes the family check. Until tickets 03 and 04 land, an Anima or FLUX.1 main model with a source is `unsupported_capability`, before any upload or enqueue.
  3. Settings, components, and the live image-to-image invocation vocabulary are checked next.
  4. An `image` source is then inspected. A `path` source is uploaded once, immediately before the enqueue, using the shared module from ticket 01. If the uploaded Image Reference reports dimensions other than those read locally, the result is `invalid_invokeai_response` and nothing is enqueued.

  Every failure after a successful upload adds `source_image` and `source_uploaded: true`, and the uploaded image is never deleted.
- **Dimensions:** An explicit `width`/`height` pair wins, and the source is resized to it. Otherwise each dimension is the source dimension rounded down to a multiple of 8. A profile's width and height do not apply when a source is present; profile applicability validation is unchanged. A source smaller than 8 pixels in either dimension is `invalid_request` before any upload or enqueue. An invalid request never leaves an uploaded image.
- **Graph:** The tested SDXL text-to-image topology, plus:
  - `i2l` with `fp32: true` and `color_compensation: "None"`, fed by the source image or by an intermediate `img_resize` (`resample_mode: "bicubic"`) when the resolved dimensions differ from the source;
  - the loader's VAE, or the `vae_loader` override, feeding both `i2l` and `l2i`;
  - `i2l` latents into `denoise_latents.latents`, with `denoising_start = 1 − strength` and `denoising_end = 1`;
  - `noise` at the resolved dimensions;
  - `core_metadata` with `generation_mode: "sdxl_img2img"`, `strength`, and `init_image` set to the source image name.

  One `enqueue_batch` produces one queue item per resolved seed and is never retried.
- **Waiting:** Each item yields exactly one non-intermediate output image; intermediate resize images are ignored.
- **Execution Receipt:** With a source, the receipt gains `source_image` and `source_uploaded`, and `resolved_settings` gains `strength`. `submitted_request` retains `source` and `strength`. Text-to-image receipts are unchanged.
- **UI Synchronization:** The Recall patch is the SDXL text-to-image patch, with no `denoise_strength`. `ui_sync_partial.not_restored` is `scheduler`, `vae`, `output_count`, `board_id`, `source_image`, `strength`.
- **Doctor:** Every `generate` entry gains `mode` (`txt2img` or `img2img`). A new `generate`/`sdxl` entry with `mode: "img2img"` requires:
  - the SDXL text-to-image requirements;
  - the image inspection and image upload endpoints;
  - `ImageResizeInvocation` (`img_resize`) with `image`, `width`, `height`, and `resample_mode`;
  - `ImageToLatentsInvocation` (`i2l`) with `image`, `vae`, `fp32`, and `color_compensation`;
  - `DenoiseLatentsInvocation` with its text-to-image properties plus `latents`;
  - `CoreMetadataInvocation` with its text-to-image properties plus `strength` and `init_image`.

  Each schema also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties. `generate` checks the same requirements before any upload or enqueue. The entry carries `ui_sync: partial` only when Recall is compatible. Existing entries, their compatibility, and existing human output lines are unchanged. The new entry is named distinctly in human output.

**Blocked by:** 01: Share Source Image resolution between upscale and generation.

**Execution route:** `frontier-owned`. This slice fixes a new public contract, records an ADR and glossary terms, and depends on live web-interface observation that automated checks cannot judge.

**Verification gate:**
- Versioned InvokeAI 6.14.1 SDXL image-to-image enqueue fixtures are checked against the live OpenAPI invocation vocabulary. They cover four cases: a source without resize, a source with resize, a VAE override, and an Output Board.
- Public-seam tests prove:
  - the local validation matrix: `strength` bounds, `strength` without `source`, both source flags, relative path, non-image file, flag and Request Document parity;
  - PNG, JPEG, and GIF `path` sources yield their header dimensions, and WebP, BMP, and ICO `path` sources are `invalid_request` before any network request;
  - an upscale `path` source of those types still behaves as before;
  - that `unsupported_capability` for Anima and FLUX.1 sources arrives before any upload;
  - network ordering: no upload before every check, a single upload, a single enqueue, no retry;
  - `outcome_unknown` for an inconclusive upload with nothing enqueued, and `source_uploaded` details on every post-upload failure path;
  - that an uploaded Image Reference with dimensions other than those read locally is `invalid_invokeai_response` with `source_uploaded` and no enqueue;
  - `not_found` for a missing `image` source;
  - dimension resolution: explicit pair, rounding down, profile pair not applied;
  - that a too-small `path` source is rejected with no upload, and a too-small `image` source with no enqueue;
  - that each missing image-to-image schema or property listed above fails `generate` as `unsupported_capability` before any upload, and marks the `doctor` entry incompatible with the property named, while the text-to-image entry stays compatible;
  - multi-output seed alignment with one shared source;
  - that intermediate resize output is ignored;
  - the receipt members, including the unchanged text-to-image receipt;
  - the `not_restored` list and the Recall-failure warning;
  - the `doctor` `mode` members and the image-to-image entry's matching and failures.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live checks against the local InvokeAI 6.14.1 baseline, following the live-verification guide, with `Juggernaut-XL-v9` (no installation needed):
  - A waited image-to-image from an `image` source. The output dimensions match the receipt, and the image metadata shows `sdxl_img2img`, `strength`, and `init_image`.
  - A PNG `path` source and a JPEG `path` source, one of them with dimensions that are not multiples of 8. Each source is uploaded once and reports the locally read dimensions. The non-aligned source is resized, and its output has the rounded-down dimensions.
  - A two-output request with an explicit seed produces seeds and images in the same order.
  - `doctor --json` reports the `img2img` entry as compatible with `ui_sync: partial`, and the text-to-image entry is unchanged.
  - Using the `browser-harness-local` skill, record the Generate tab (and the Canvas bounding box, if affected) after Recall and model loading. Record whether the image metadata panel recalls `strength`.
  - The live E2E gate (`BEDIZ_E2E_URL` set) passes and gains a self-cleaning SDXL image-to-image case that removes both its output and any source it uploaded.
- Report every live step that was unavailable. List ad hoc images, including uploaded sources.

**Review gate:** Not required by this route.

**Escalate when:**
- InvokeAI 6.14.1 rejects the recorded graph, or needs an input the feature spec does not record.
- The resize image appears as a non-intermediate output.
- InvokeAI records different dimensions than the PNG, JPEG, or GIF header for a tested file (for example, because of EXIF orientation).
- The output dimensions differ from the resolved dimensions.
- The web interface interprets `strength` metadata differently from the recorded meaning.
- Recall of dimensions has an effect on the Canvas that the spec's Handoff description would misstate.
- Representing the mode in `doctor` would change an existing entry's compatibility or an existing human output line.
- Repeated repair loops fail.

**Permanent records:**
- New ADR-0023 "Express image-to-image as a Source Image on the Generation Request". It records these decisions: `generate` is extended instead of adding a command; the Generation Mode is inferred from the inputs; dimensions come from the source and profile dimensions are excluded; strength has InvokeAI's web-interface meaning; `doctor` has one entry per mode. It lists a separate command as a considered and rejected option.
- `CONTEXT.md`: add **Denoising Strength** and **Generation Mode**, and relate **Source Image** to generation as well as upscale.
- V1 spec:
  - §2 records image-to-image as an outcome.
  - §9 records the `strength` error location.
  - §10 records the `mode` member and the SDXL image-to-image entry.
  - A new §11 subsection records the request, order, dimensions, graph, metadata, and receipt rules, with the SDXL row.
  - §12 records the `not_restored` additions.
- `CHANGELOG.md` under Unreleased. Tests.
- The live-verification guide's installed-families list.

**Status:** ready-for-agent

- [ ] `generate` with an SDXL main model and a Source Image produces image-to-image outputs through one tested enqueue, with the recorded dimensions, strength, metadata, and receipt.
- [ ] A `path` source is uploaded once, only after every check, and is reported on every later failure without being deleted.
- [ ] Anima and FLUX.1 sources fail as `unsupported_capability` before any upload, and text-to-image behavior and receipts are unchanged.
- [ ] `doctor`, `ui_sync_partial`, ADR-0023, `CONTEXT.md`, the V1 spec, and `CHANGELOG.md` reflect the behavior verified live, and every unavailable live step is reported.
