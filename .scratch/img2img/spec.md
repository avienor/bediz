# Image-to-image generation

**Status:** Approved ticket plan (2026-09-28). Tickets 01–05 are ready for agent in dependency order.

This feature adds image-to-image generation to `generate` for the registered generation families Anima, SDXL, and FLUX.1. It is the first post-V1 creative operation taken from the backlog of work V1 left out. It is also the smallest durable piece of what InvokeAI's Canvas does. The Canvas composites its layers in the browser and then submits an ordinary text-to-image, image-to-image, inpaint, or outpaint graph. Its layer state lives in frontend-private client state, which Bediz does not manipulate (V1 spec §12, ADR-0001, ADR-0003). Image-to-image introduces the Source Image and Denoising Strength concepts, which inpainting and outpainting later build on.

The accepted product boundary is the V1 specification, in particular §8–12, §15.1, §17, and §25. Together with it: ADR-0003 (Contract Parity), ADR-0008 (tested versions and unsafe retries), ADR-0009 (Execution Receipts), ADR-0010 (explicit Output Board), ADR-0011 (typed, family-aware core generation), ADR-0012 (source upload semantics, as established for upscale), ADR-0017 (verified Recall only), and ADR-0022 (schema version 1 stays additive). Every change here is additive under schema version 1. Each implementing ticket moves the behavior it delivers into the V1 specification. A contradiction with the V1 specification or an ADR is an escalation, not permission to silently change the contract.

## Decisions

Recorded with the user on 2026-09-28.

### Public shape

- Image-to-image extends `generate`; it is not a separate command. The Generation Request gains two optional members:
  - `source`, with exactly the upscale shape: `{"type":"image","reference":"<InvokeAI image name>"}` or `{"type":"path","reference":"<absolute local path>"}`. The flags are `--image` and `--image-path`.
  - `strength` (flag `--strength`).
- The Generation Mode follows from the inputs. Without `source` the request is text-to-image and behaves exactly as today. With `source` it is image-to-image. The Result Envelope operation stays `generate`. A later inpainting feature is expected to add a mask to the same request.
- Supplying both `--image` and `--image-path`, or mixing operation flags with `--request`, is `invalid_request`, as for upscale. Request Documents follow the §8.1 strict rules.
- `strength` is the Denoising Strength, with InvokeAI's web-interface meaning. It must be finite and satisfy `0 < strength ≤ 1`. It defaults to `0.75`, InvokeAI 6.14.1's UI default. Zero is invalid because every tested denoiser requires `denoising_start < denoising_end`. `strength` without `source` is `invalid_request`. Every `strength` error names `strength` in `error.details.field`. Source validation errors keep the upscale behavior.
- Generation Profiles gain no `strength` field in this feature.

### Output dimensions

- An explicit `width` and `height` pair wins. The source is resized to exactly that size, without preserving its aspect ratio.
- Otherwise each dimension is the source dimension rounded down to the family alignment: 8 for Anima and SDXL, 16 for FLUX.1. For example, a 1000 × 750 source becomes 1000 × 744 for SDXL and 992 × 736 for FLUX.1.
- A profile's `width` and `height` are not applied when a source is present. With a source, the source replaces both the family default and the profile pair as the dimension default. Profile applicability validation is unchanged, so an inapplicable profile dimension is still `invalid_request`.
- Source dimensions come from InvokeAI's Image Reference for an `image` source, and from the local file for a `path` source.
- For a `path` source, Bediz reads the width and height from the PNG, JPEG, or GIF header before any network request, using only the Go standard library. `images upload` also accepts WebP, BMP, and ICO. As an image-to-image `path` source, those types are `invalid_request` before any network request. The caller can upload such a file with `images upload` and pass its name as an `image` source. Upscale `path` sources are unaffected.
- After a `path` upload, InvokeAI's reported dimensions must equal the dimensions read locally. A difference is `invalid_invokeai_response`, with `source_image` and `source_uploaded: true`, and nothing is enqueued.
- A source smaller than the family alignment in either dimension is `invalid_request` before any upload or enqueue, for both source kinds. An invalid image-to-image request never leaves an uploaded image, as V1 §13.2 requires for upscale.
- When the resolved dimensions differ from the source dimensions, the graph resizes the source with an intermediate `img_resize` node using `resample_mode: "bicubic"`. When they are equal, the source feeds the encoder directly.
- Output dimensions are not verified after completion. There is no counterpart to upscale's `scale_not_applied`, matching text-to-image.

### Validation and execution order

1. **Before any network request:** every existing local generation check, the `source` shape, and the `strength` bounds and applicability. For a `path` source, also the absolute-path, readable-regular-file, and image-content checks of `images upload` (§15.1), and reading the dimensions from the PNG, JPEG, or GIF header.
2. **After main-model resolution and before any upload or enqueue:** the family must support image-to-image, otherwise `unsupported_capability`. Also in this step:
   - family settings, including explicit-dimension alignment;
   - component resolution;
   - the live OpenAPI check of the image-to-image invocation vocabulary;
   - for a `path` source, the too-small check and dimension resolution from the locally read size.
3. **Source confirmation.**
   - An `image` source is read with image inspection. A missing image is `not_found`, and a contradictory image name is `invalid_invokeai_response`. The too-small check and dimension resolution follow. All of this happens before enqueue.
   - A `path` source is uploaded once, immediately before the enqueue, with the same semantics as upscale (§13.2). The upload is never retried. An inconclusive upload is `outcome_unknown` and nothing is enqueued. The uploaded Image Reference must report the locally read dimensions.
   - After a successful upload, every later failure keeps its own error code and adds `source_image` and `source_uploaded: true`. Bediz never deletes the uploaded image.
4. **Enqueue.** One `enqueue_batch` mutation to the default queue, never retried, produces one queue item per resolved seed. Every item uses the same source.

Waiting follows §11.5–11.6. It selects each item's single non-intermediate output image and ignores intermediate resize results.

### Graphs (InvokeAI 6.14.1)

Each family keeps its tested text-to-image topology and adds the stock frontend's image-to-image encoder path. The VAE that feeds the decoder also feeds the encoder, and the encoder's latents feed the denoiser's `latents` input. `denoising_end` stays 1.

| Family | Encoder node | `denoising_start` | Metadata `generation_mode` |
| --- | --- | --- | --- |
| SDXL | `i2l` with `fp32: true` and `color_compensation: "None"` | `1 − strength` | `sdxl_img2img` |
| Anima | `anima_i2l` | `1 − strength` | `anima_img2img` |
| FLUX.1 | `flux_vae_encode` | `1 − strength^0.2` | `flux_img2img` |

- **SDXL** keeps the `noise` node at the resolved dimensions. An optional `vae_loader` override feeds both `i2l` and `l2i`.
- **FLUX.1** uses the frontend's default "optimized denoising" mapping, so a strength means the same as in the web interface and in its metadata recall. Variant rules are unchanged from text-to-image. `dev_fill` is rejected. Kontext dev and Krea dev report variant `dev` and are accepted by variant. Bediz does not replicate the frontend's display-name block on Kontext, because it never infers purpose from a name. A non-empty negative prompt, or guidance for schnell, stays `invalid_request`.
- **Anima** keeps Bediz's tested text-to-image conditioning shape, including the negative conditioning branch.
- **Metadata:** `core_metadata` additionally records the resolved `strength` and `init_image` (the source image name). The stock web interface's image metadata panel can recall `strength` from it.

### Execution Receipt

- `submitted_request` retains `source` and `strength` as submitted.
- For a request with a source, the receipt gains `source_image` (the Image Reference) and `source_uploaded` (`true` only when Bediz uploaded a `path` source in this request). `resolved_settings` gains `strength`. `resolved_settings.width` and `height` are the resolved output dimensions.
- Text-to-image receipts are unchanged: these members are omitted.

### UI Synchronization

- Automatic Recall sends the same fields as the family's text-to-image generation, and sends nothing else. For every family that means prompts, model, dimensions, steps, and the first seed; SDXL also sends `cfg_scale`. InvokeAI 6.14.1's Recall API accepts `denoise_strength`, but the stock frontend's Recall handler does not apply it, so Bediz does not send it.
- `ui_sync_partial.not_restored` is the family's text-to-image list followed by `source_image` and `strength`.
- Standalone `recall` is unchanged.

### Capability reporting

- Every `generate` capability entry in `doctor` gains a `mode` member: `txt2img` or `img2img`.
- Each family with image-to-image support has a separate `mode: "img2img"` entry. Its requirements are the family's text-to-image requirements plus:
  - the image inspection endpoint (`GET /api/v1/images/i/{image_name}`) and the upload endpoint (`POST /api/v1/images/upload`);
  - every input property the image-to-image graph sets or connects, as listed below;
  - `core_metadata`'s `strength` and `init_image` properties.
- Model requirements match the text-to-image entry. The entry carries `ui_sync: partial` only when Recall is compatible.
- `generate` checks the same requirements before any upload or enqueue. A missing schema or property fails as `unsupported_capability` naming it. The input properties below were confirmed against the live InvokeAI 6.14.1 OpenAPI on 2026-09-28; each entry also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties.

| Schema (type) | Families | Required input properties |
| --- | --- | --- |
| `ImageResizeInvocation` (`img_resize`) | all | `image`, `width`, `height`, `resample_mode` |
| `ImageToLatentsInvocation` (`i2l`) | SDXL | `image`, `vae`, `fp32`, `color_compensation` |
| `DenoiseLatentsInvocation` (`denoise_latents`) | SDXL | text-to-image properties plus `latents` |
| `AnimaImageToLatentsInvocation` (`anima_i2l`) | Anima | `image`, `vae` |
| `AnimaDenoiseInvocation` (`anima_denoise`) | Anima | text-to-image properties plus `latents` |
| `FluxVaeEncodeInvocation` (`flux_vae_encode`) | FLUX.1 | `image`, `vae` |
| `FluxDenoiseInvocation` (`flux_denoise`) | FLUX.1 | text-to-image properties plus `latents`, `denoising_start`, `denoising_end`, `add_noise` |
| `CoreMetadataInvocation` (`core_metadata`) | all | text-to-image properties plus `strength`, `init_image` |
- Text-to-image entries and their compatibility are unchanged. The `ui_sync.generate` rule is unchanged.
- Human `doctor` output keeps the existing lines and names image-to-image entries distinctly.

### Terminology

`CONTEXT.md` gains the following entries; ticket 02 finalizes their wording:

- **Denoising Strength:** the portion of the denoising schedule that an image-to-image generation regenerates from its Source Image, with InvokeAI's web-interface meaning. Values near 0 keep the source; 1 regenerates it completely. _Avoid:_ creativity, denoising start, image weight.
- **Generation Mode:** the kind of generation a Generation Request performs, determined by its inputs: text-to-image without a Source Image, image-to-image with one. _Avoid:_ Canvas mode, UI tab.

## Evidence from InvokeAI 6.14.1

These findings come from the installed 6.14.1 package, the backend invocations and the built frontend bundle. The live-verification guide says where they are. Search the bundle for `sdxl_img2img`, `anima_img2img`, `flux_img2img`, and `img2imgStrength`.

- **Graph builder.** The Canvas graph builder asks its compositor for a generation mode (`txt2img`, `img2img`, `inpaint`, `outpaint`), then builds one ordinary graph per model base. For image-to-image, the composite of the visible raster layers is the encoder's image. It is resized in and the output resized out when the bounding box is scaled.
- **Strength mapping.** The frontend maps strength to `denoising_start`:
  - `1 − s` for Anima, SD1.5, and SDXL without a refiner;
  - `1 − s^0.2` for FLUX when optimized denoising is enabled (the default), and `1 − s` when it is disabled;
  - `0` for FLUX `dev_fill`, which is incompatible with image-to-image anyway.

  The default strength is 0.75.
- **SDXL encoder.** It is `i2l` with `fp32` from the VAE precision (default fp32) and `color_compensation` `None` by default.
- **Anima denoiser.** `anima_denoise` documents image-to-image through its `latents` input. It raises an error when `denoising_start ≥ denoising_end`. `anima_i2l` is classified as a Prototype invocation.
- **Recall.** The Recall API schema includes `denoise_strength` (0–1). The frontend's `recall_parameters_updated` handler applies prompts, model, dimensions, steps, seed, `cfg_scale`, LoRAs, control layers, IP adapters, and reference images, but not `denoise_strength`.
- **Metadata.** The metadata viewer registers a `DenoisingStrength` handler that parses `strength` from image metadata and recalls it. `core_metadata` accepts `strength` and `init_image`.
- **Upload.** The image upload route opens the file with PIL and applies no EXIF orientation; no `exif_transpose` call exists in the 6.14.1 package. Bediz sends no crop or resize options. The recorded width and height are therefore the file header's, which is why a `path` source's dimensions can be read locally before the upload.
- **Go content detection.** `images upload` relies on Go's detection, which identifies ICO, BMP, GIF, WebP, PNG, and JPEG as images. The Go standard library decodes PNG, JPEG, and GIF headers only.

## Live verification baseline

On 2026-09-28 the local InvokeAI 6.14.1 baseline had every model this feature needs:

- SDXL `Juggernaut-XL-v9` with `sdxl-vae-fp16-fix`;
- `Anima Base 1.0` with its QwenImage VAE and Qwen3 0.6B encoder;
- FLUX.1 dev and schnell BnB NF4 with the T5 int8 encoder, CLIP Embed, and `FLUX.1-schnell_ae`.

No installation is needed, and none is consented. Source images come from Bediz generations on the baseline or from local test files in a scratch directory. Follow the live-verification guide. Use 768 × 768 sources where the behavior allows it. List every ad hoc image, including uploaded sources, so the user can remove them. The guide's list of installed families is out of date; ticket 02 corrects it.

## Known live risks

- A Recall that changes the main model triggers asynchronous model loading in the open web interface, which may reset other controls. Record the final observed state.
- Recall of width and height can interact with Canvas bounding-box state. Ticket 02 live observation on an empty Canvas found its existing 2512 × 416 bounding box unchanged after Recall changed Generate dimensions to 768 × 512 and then 512 × 512. Record both surfaces for each future mode and Canvas state tested.
- 8 GB VRAM can fail FLUX.1 or large-source generations. A resource failure is a live-environment limit to report, not a graph defect.
- Output dimensions differ from the source when the family alignment rounds them down. Receipts must show the resolved values.

## Out of scope

- Inpainting, outpainting, and masks.
- Canvas layer or brush control, and Handoff into the Canvas staging area.
- Image-to-image for SD1.5 or other unregistered families.
- The SDXL refiner, PID and DyPE modes, HiDiffusion, and tiled VAE encoding.
- A color-compensation or resample-mode setting.
- Reference images, FLUX Kontext editing, and FLUX Fill.
- Profile `strength`, and Recall of `strength` or the source image.
- More than one source per request.
- WebP, BMP, and ICO as image-to-image `path` sources. They can be added later without a schema change.
- The existing text-to-image `generate/flux` entry does not check the `flux_denoise` properties `denoising_start`, `denoising_end`, and `add_noise`, although its graph sends them. That pre-existing gap is outside this feature.

## Revisions

- **2026-09-28, after plan review:**
  - `path` source dimensions are read locally before any network request (PNG, JPEG, and GIF only, a user decision), so an invalid request never leaves an uploaded image.
  - The `doctor` and preflight input properties are enumerated.
  - Ticket 05's retry guidance now follows the error code, so a retry cannot duplicate an enqueue whose outcome is unknown.

## Tickets

1. `issues/01-share-source-image-resolution.md`: Share Source Image resolution between upscale and generation. Blocked by nothing.
2. `issues/02-sdxl-image-to-image.md`: Generate SDXL images from a Source Image. Blocked by 01.
3. `issues/03-anima-image-to-image.md`: Generate Anima images from a Source Image. Blocked by 02.
4. `issues/04-flux1-image-to-image.md`: Generate FLUX.1 images from a Source Image. Blocked by 02.
5. `issues/05-skill-and-readme-image-to-image.md`: Teach the agent skill and README image-to-image. Blocked by 02, 03, and 04.
