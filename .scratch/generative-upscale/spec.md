# SD1.5 and SDXL generative upscale

**Status:** Approved ticket plan on 2026-09-23; tickets 01–05 are completed as of 2026-09-24. Revised the same day after an independent plan review: shared main-model selection, the output-dimension rule and `scale_not_applied` failure (user decision), and ticket 04's failure coverage and upload endpoint requirement.

This feature delivers V1 delivery step 7. The accepted product boundary is Bediz V1 specification sections 5, 6, 8–10, 12, 13, 15, 17, 18, 22, and 23, together with ADR 0006 (Request Documents), ADR 0007 (Result Envelope), ADR 0008 (tested versions and unsafe retries), ADR 0009 (Execution Receipts), ADR 0010 (explicit Output Board), ADR 0011 (typed, family-aware settings), ADR 0012 (InvokeAI's generative upscale flow), ADR 0015 (6.14 baseline and vertical slices), and ADR 0017 (verified Recall on stock InvokeAI). This file records the decisions needed to execute the step. Each implementing ticket moves the behavior it delivers into the V1 specification. A contradiction with the V1 specification or an ADR is an escalation, not permission to silently change the contract.

## Scope

Step 7 adds `bediz upscale`: InvokeAI 6.14.1's tiled multi-diffusion Generative Upscale for SD1.5 and SDXL main models, from an existing InvokeAI image or an absolute local image path. It includes automatic UI Synchronization and the `doctor` capability entries `upscale/sdxl` and `upscale/sd-1`. Out of scope: LoRAs, the Upscale panel's post-processing model, multiple outputs, Generation Profile upscale settings (delivery step 8), SD1.5 text-to-image generation, and standalone `recall` for SD1.5 models.

## Stock InvokeAI 6.14.1 reference

The stock frontend's Upscale tab graph builder is the source of truth for graph topology and defaults. It lives in the built bundle under `frontend/web/dist/assets/` of the installed InvokeAI package. Search it for `tiled_multi_diffusion_denoise_latents`. The upscale slice's initial state is in the same directory (search for `tileOverlap:128`). The invocation field constraints are in `app/invocations/tiled_multi_diffusion_denoise_latents.py` and `app/invocations/spandrel_image_to_image.py`.

## Request Document and flags

The schema-version-1 Upscale Request Document has these fields. Unknown fields are `invalid_request`. Operation flags cannot be combined with `--request`. Connection, output, `--no-wait`, and `--timeout` flags may accompany it.

| Field | Flag | Rule | Default |
| --- | --- | --- | --- |
| `schema_version` | — | required, `1` | — |
| `source` | `--image NAME` or `--image-path PATH` | required typed object `{"type": "image" \| "path", "reference": "<string>"}`; exactly one of the two flags | — |
| `model` | `--model` | required main-model Model Key or unique name | — |
| `positive_prompt` | `--prompt` | optional string | `""` |
| `negative_prompt` | `--negative-prompt` | optional string | `""` |
| `scale` | `--scale` | integer `2`, `4`, or `8` | `4` |
| `creativity` | `--creativity` | integer from −10 to 10 | `0` |
| `structure` | `--structure` | integer from −10 to 10 | `0` |
| `steps` | `--steps` | positive integer | `30` |
| `scheduler` | `--scheduler` | one of the 31 InvokeAI 6.14.1 `SCHEDULER_NAME_VALUES` already listed for SDXL in V1 §11.2 | `kdpm_2` |
| `guidance` | `--guidance` | CFG scale; finite and at least 1 | `2.0` |
| `seed` | `--seed` | unsigned 32-bit integer; random when omitted | random |
| `tile_size` | `--tile-size` | multiple of 64 from 512 to 1536 | `1024` |
| `tile_overlap` | `--tile-overlap` | multiple of 8 from 16 to 512, and less than `tile_size` | `128` |
| `board_id` | `--board` | optional Output Board identifier | none (Uncategorized) |
| `components.upscale_model` | `--upscale-model` | optional Spandrel selector | automatic |
| `components.tile_controlnet` | `--tile-controlnet` | optional Tile ControlNet selector | candidates returned |
| `components.vae` | `--vae` | optional VAE override | main model's bundled VAE |

- The CFG field is named `guidance`, matching `generate`, where SDXL `guidance` is also CFG scale.
- `source.type: "image"` names an exact existing InvokeAI image. `source.type: "path"` names an absolute image path on the CLI machine. This differs from `models install --source-type path`, which names a path on the InvokeAI server.
- `scale` accepts only the stock Upscale panel's 2, 4, and 8 (user decision, 2026-09-23). InvokeAI accepts any scale in (0, 16]; that wider range stays unsupported until tested.
- Each upscale produces exactly one output. There is no `output_count` field.
- Every numeric bound above is family-independent, so it is checked before any network request, together with schema version, the required `source` and `model`, source shape, and unknown fields. An explicitly empty selector string is `invalid_request`.

## Validation and resolution order

1. Local checks (above). For a `path` source, the path must be absolute and name a readable regular file.
2. The InvokeAI version is in the supported range. Otherwise `unsupported_capability`.
3. The `model` selector resolves across installed main models by exact Model Key or unique name. A shared name returns `selection_required` with candidates sorted by Model Key. This selection is shared with generation but independent of any operation's family registry. Each operation then applies its own family acceptance. For upscale, a non-main key, a base other than a registered upscale family (`sdxl` in ticket 02; `sd-1` added by ticket 03), or a recorded Model Variant other than `normal` returns `unsupported_capability`. Generation keeps its own registry, so an SD1.5 main model is still unsupported for `generate`.
4. Upscale Component Set resolution (below).
5. The live OpenAPI document exposes the family's tested invocation vocabulary.
6. The source is confirmed. An `image` source is read with image inspection and a missing image is `not_found`. A `path` source is uploaded (ticket 04).
7. One `enqueue_batch` mutation to the `default` queue. Never retried.

## Upscale Component Set

- **Spandrel upscale model** (`upscale_model`): base `any`, type `spandrel_image_to_image`. Resolved by the V1 §11.4 rules: an explicit compatible key or unique name wins; otherwise exactly one compatible installed model is selected; several return `selection_required` (kind `upscale_model`, empty selector, candidates sorted by Model Key); none returns `missing_component` with install guidance naming the `RealESRGAN_x4plus` starter. InvokeAI records no scale factor for a Spandrel model, and stock autoscale ignores `scale` for a model that does not enlarge the image. Bediz therefore cannot prove before execution that a Spandrel model upscales. It keeps automatic selection and verifies the completed output dimensions instead (see Output dimensions). User decision, 2026-09-23.
- **Tile ControlNet** (`tile_controlnet`): type `controlnet` with the main model's base. InvokeAI records no Tile kind for a ControlNet, so Bediz cannot verify that an installed ControlNet is a Tile model. An explicit key or unique name of a base-compatible ControlNet is accepted as the caller's choice. Without a selector, Bediz never selects automatically: it returns `selection_required` with kind `tile_controlnet`, an empty selector, and every base-compatible ControlNet as a candidate sorted by Model Key, even when only one exists. With none installed, it returns `missing_component` with install guidance naming the stock `Tile` starter source for that base (`xinsir/controlNet-tile-sdxl-1.0` for SDXL, `lllyasviel/control_v11f1e_sd15_tile` for SD1.5). User decision, 2026-09-23. This keeps ADR 0012's unambiguous-resolution rule; it is not a new ADR.
- **VAE** (`vae`): optional override with the main model's base and type `vae`, resolved by exact key or unique name, exactly as the SDXL generation VAE override. Without it the main model's bundled VAE is used and no VAE is selected automatically.
- A mismatched base or type for an explicit selector is `unsupported_capability`; an ambiguous explicit name is `selection_required`.
- `resolved_settings.component_keys` always contains `upscale_model` and `tile_controlnet`, and contains `vae` only for an override.

## Graph contract

The graph matches the stock 6.14.1 Upscale tab builder with LoRAs omitted:

- `string` positive and negative prompt nodes and an `integer` seed node. The seed flows through the batch data exactly as in generation.
- `spandrel_image_to_image_autoscale` with the source image, the Spandrel model, `scale`, and `fit_to_multiple_of_8: true`, feeding `unsharp_mask` (radius 2, strength 60).
- `noise`, taking width and height from the sharpened image and the seed.
- Tiled `i2l` and a non-intermediate tiled `l2i`, both with `tile_size` and `fp32: true`. The `l2i` carries the optional board.
- `tiled_multi_diffusion_denoise_latents` with `tile_width` and `tile_height` equal to `tile_size`, `tile_overlap`, `steps`, `cfg_scale` from `guidance`, `scheduler`, `denoising_start = (10 − creativity) × 4.99 / 100`, and `denoising_end: 1`.
- Two `controlnet` nodes on the sharpened image, both using the Tile ControlNet with `control_mode: balanced` and `resize_mode: just_resize`, collected into the denoiser's `control` input:
  - first: weight `(structure + 10) × 0.0325 + 0.3`, begin 0, end `(structure + 10) × 0.025 + 0.3`;
  - second: weight `((structure + 10) × 0.0325 + 0.15) × 0.45`, begin `(structure + 10) × 0.025 + 0.3`, end 0.85.
- SDXL: `sdxl_model_loader` with positive and negative `sdxl_compel_prompt`, whose style inputs equal their prompts.
- SD1.5: `main_model_loader`, a `clip_skip` node with 0 skipped layers, and positive and negative `compel`. Clip skip is not a public setting.
- An optional `vae_loader` feeds `i2l` and `l2i` when a VAE override is present; otherwise the main loader's VAE does.
- `core_metadata` records the stock upscale fields: prompts, seed, output width and height from the Spandrel node, steps, scheduler, CFG scale, main model, optional VAE, `upscale_model`, `creativity`, `structure`, `tile_size`, `tile_overlap`, `upscale_initial_image`, and `upscale_scale`. It is attached to `l2i`.
- Complete InvokeAI Model Identifiers are used throughout. Node identifiers may vary without changing this contract.

## Output dimensions

Stock `spandrel_image_to_image_autoscale` targets `int(source × scale)` in each dimension, then, with `fit_to_multiple_of_8: true`, rounds down to a multiple of 8. The expected output is therefore `floor(source_width × scale / 8) × 8` by `floor(source_height × scale / 8) × 8`. For example, a 513 × 513 source at scale 2 produces 1024 × 1024, not 1026 × 1026. The source dimensions come from the source Image Reference: from image inspection for an `image` source, and from the upload result for a `path` source.

After waiting, Bediz compares the completed output Image Reference with the expected dimensions. A mismatch, for example from a Spandrel model that does not enlarge the image, is never a successful receipt. It returns `invokeai_operation_failed` (exit status 6) with details: `reason: "scale_not_applied"`, the expected and actual dimensions, the queue identifiers, the output Image Reference, and the source Image Reference. An accepted `--no-wait` receipt carries the expected dimensions but performs no verification.

## Execution Receipt

A completed upscale returns the V1 Result Envelope with `operation: "upscale"` and this data:

- `submitted_request`: the canonical Upscale Request Document before defaults and component resolution.
- `source_image`: the source Image Reference, and `source_uploaded`: whether Bediz uploaded it in this request.
- `resolved_settings`: `positive_prompt`, `negative_prompt`, `scale`, `creativity`, `structure`, `steps`, `scheduler`, `guidance`, `tile_size`, `tile_overlap`, the expected `output_width` and `output_height`, optional `board_id`, `model_key`, `component_keys`, and a one-element `seeds` array.
- `queue`: `queue_id`, `batch_id`, and one `item_ids` entry.
- `outputs`: one item, seed, and Image Reference association, verified against the queue item's batch seed as in generation. It is empty for `--no-wait`.
- `warnings`: UI Synchronization warnings (ticket 05).

Waiting, `--timeout`, interruption, `wait_timeout`, `interrupted`, failed or canceled items, unknown queue statuses, and image-output verification behave exactly as for generation in V1 §11.5–11.6.

## Local source upload

- Validation and resolution run before any upload, so an invalid request never leaves an uploaded image.
- The upload is a single non-intermediate user image upload without a board, following `images upload`. It is never retried. An inconclusive upload returns `outcome_unknown` and nothing is enqueued.
- After a successful upload, every later failure reports the complete uploaded Image Reference in its error details. This covers enqueue rejection, an inconclusive or malformed enqueue response (`outcome_unknown`), a malformed or unknown queue response (`invalid_invokeai_response`), a failed or canceled item, `scale_not_applied`, `wait_timeout`, and `interrupted`. Bediz never deletes it automatically.

## UI Synchronization

- After a successful enqueue, Bediz sends one Recall patch with the positive and negative prompts, the main model, steps, and the seed. It never retries it. The stock 6.14.1 Upscale tab shares these values with the Generate tab. The patch omits width and height, which do not apply to the Upscale panel, and `cfg_scale`, which stock Recall writes to the Generate tab's CFG instead of the upscale CFG.
- The main model uses the existing display-name uniqueness rule. A collision or a Recall failure adds `ui_sync_failed` and leaves the upscale successful.
- A successful patch adds `ui_sync_partial` whose `not_restored` list is `source_image`, `upscale_model`, `scale`, `creativity`, `structure`, `tile_controlnet`, `tile_size`, `tile_overlap`, `scheduler`, `guidance`, `vae`, and `board_id`, adjusted only by live browser evidence.
- `doctor` reports `ui_sync: partial` on the upscale capability entries and `ui_sync.upscale: partial` only when Recall is compatible.
- Standalone `recall` does not accept SD1.5 main models; SD1.5 is not a generation family in V1.

## Capability reporting

`doctor` registers `upscale/sdxl` (ticket 02) and `upscale/sd-1` (ticket 03) with the enqueue, image, and version endpoints; the tested invocation vocabulary; and these installed-model requirements: a main model of that base with variant `normal`, a Spandrel image-to-image model, and a ControlNet of that base. These requirements establish only presence. The ControlNet requirement cannot establish that the ControlNet is a Tile model, and the Spandrel requirement cannot establish that the model enlarges images. From ticket 04, both entries also require the tested image upload endpoint (`POST /api/v1/images/upload`), because the upscale contract includes local-file sources. Stock 6.14.x always exposes it.

## Live verification baseline and Installation Consent

On 2026-09-23 the user approved installing these models into the local InvokeAI 6.14.1 baseline (RTX 4060, 8 GB VRAM) for live verification, through `bediz models install --source-type starter`:

- `RealESRGAN_x4plus` (Spandrel, BSD-3-Clause, about 64 MB).
- SDXL `Tile` ControlNet, source `xinsir/controlNet-tile-sdxl-1.0` (about 2.5 GB).
- `Dreamshaper 8`, SD1.5 main, source `Lykon/dreamshaper-8` (about 2 GB, CreativeML OpenRAIL-M).
- SD1.5 `Tile` ControlNet, source `lllyasviel/control_v11f1e_sd15_tile` (about 1.4 GB).

The SDXL main model `Juggernaut-XL-v9` and its `sdxl-vae-fp16-fix` VAE are already installed. Both `Tile` starters share a display name; select each by its exact source. Live verification must not install any other model without new consent.

## Known live risks

- Generative upscale is VRAM-heavy. Use a small source image (for example 512 × 512 at scale 2) for ad-hoc checks. An out-of-memory failure is an environment limit to report, not a graph defect.
- Recall can change the main model in the open UI, and the frontend loads it asynchronously. Browser verification must observe the final Upscale tab state.
