# SDXL and FLUX.1 generation adapters

**Status:** Approved ticket plan; ticket 01 is implemented, and tickets 02–04 are ready for agent. The recognized inapplicable-component test remains in ticket 04 by user decision on 2026-09-23.

This feature delivers V1 delivery step 6. The accepted product boundary is Bediz V1 specification sections 5, 8–12, 17, 22, and 23, together with ADR 0006 (Request Documents), ADR 0007 (Result Envelope), ADR 0008 (tested versions and unsafe retries), ADR 0009 (Execution Receipts), ADR 0010 (explicit Output Board), ADR 0011 (typed, family-aware core generation), ADR 0015 (6.14 baseline and vertical slices), and ADR 0017 (verified Recall on stock InvokeAI). This file records the decisions needed to execute the step. Each implementing ticket moves the behavior it delivers into the V1 specification. A contradiction with the V1 specification or an ADR is an escalation, not permission to silently change the contract.

## Scope

Step 6 adds SDXL and FLUX.1 text-to-image generation to `generate`, together with their automatic UI Synchronization, standalone `recall` acceptance of their main models, and `doctor` capability entries. It also extends step 5's starter installation to catalog entries with Hugging Face subfolder sources, so that a Bediz-only agent can install FLUX.1 with its components. Anima behavior stays unchanged, except that family-specific validation now runs after the main model is resolved (see below). Out of scope: the SDXL refiner, FLUX.1 Fill, true-CFG FLUX generation, LoRAs, ControlNet, img2img, inpainting, SDNQ self-contained FLUX pipelines, Generation Profile preferences, and generative upscale (delivery step 7).

## Family selection and validation order

- The `model` selector (Model Key or unique name) resolves against every installed main model. The resolved model's base selects the Model Family adapter: `anima`, `sdxl`, or `flux`. A main model of any other base (including `sdxl-refiner`) returns `unsupported_capability`. An ambiguous name returns `selection_required` with candidates sorted by Model Key, as today.
- Family-independent checks run before any network request: schema version, required `model` and `positive_prompt`, width and height supplied together, and unknown fields.
- Family-specific checks run after main-model resolution and before the enqueue mutation: dimension alignment, scheduler set, guidance applicability and bounds, negative-prompt applicability, and component applicability. They still fail as `invalid_request` (exit 2). For Anima this means an invalid scheduler, for example, is now reported after Bediz connects to InvokeAI instead of before. The exit status and error code do not change.
- A component selector that the resolved family does not use (for example `qwen3_encoder` with SDXL) is `invalid_request`. It is never ignored.
- Registering a family adapter enables `generate`, automatic UI Synchronization, standalone `recall` main-model acceptance, and the family's `doctor` capability entry together. No family is advertised before its ticket's fixtures and live checks exist.

## SDXL

- The main model must have base `sdxl` and type `main`. Any format accepted by InvokeAI's SDXL loader is allowed; the capability is recorded at the family level.
- Defaults: 1024 × 1024, 30 steps, scheduler `dpmpp_3m_k`, guidance 7.0, one output, empty negative prompt.
- Width and height are positive multiples of 8. Steps are positive. `guidance` is SDXL CFG scale; it must be finite and at least 1. The scheduler set is InvokeAI 6.14.1's `SCHEDULER_NAME_VALUES`: `ddim`, `ddpm`, `deis`, `deis_k`, `lms`, `lms_k`, `pndm`, `heun`, `heun_k`, `euler`, `euler_k`, `euler_a`, `kdpm_2`, `kdpm_2_k`, `kdpm_2_a`, `kdpm_2_a_k`, `dpmpp_2s`, `dpmpp_2s_k`, `dpmpp_2m`, `dpmpp_2m_k`, `dpmpp_2m_sde`, `dpmpp_2m_sde_k`, `dpmpp_3m`, `dpmpp_3m_k`, `dpmpp_sde`, `dpmpp_sde_k`, `er_sde`, `unipc`, `unipc_k`, `lcm`, `tcd`.
- The negative prompt is supported. The SDXL style prompts equal the positive and negative prompts, which matches the stock frontend's default of concatenating prompt and style.
- The only component is an optional `components.vae` override (base `sdxl`, type `vae`), resolved by exact key or unique name. When it is omitted, the main model's bundled VAE is used and no automatic VAE selection happens, even if exactly one SDXL VAE is installed. §11.4's automatic resolution applies only to required components. `resolved_settings.component_keys` is empty without an override and `{"vae": "<key>"}` with one.
- The graph shape is the stock InvokeAI 6.14.1 SDXL text-to-image topology: `sdxl_model_loader`, positive and negative `sdxl_compel_prompt`, `noise`, `denoise_latents`, `core_metadata`, and a non-intermediate `l2i` output node. A `vae_loader` is added only for an override. The seed flows through the batch data exactly as in the Anima adapter, keeping `item_ids` and `resolved_settings.seeds` aligned by position.
- UI Synchronization: generation Recall additionally sends `cfg_scale` (from the resolved guidance), because the stock 6.14.1 frontend Recall handler applies it. This is accepted only if live browser verification confirms it. `ui_sync_partial.not_restored` for SDXL is `scheduler`, `vae`, `output_count`, and `board_id`, plus `guidance` if live verification shows that CFG is not restored.

## FLUX.1

- The main model must have base `flux`, type `main`, and InvokeAI variant `dev` or `schnell`. Variant `dev_fill`, a missing or unknown variant, and format `sdnq_quantized` return `unsupported_capability`. FLUX.1 Krea dev and Kontext dev models report variant `dev` and are accepted for text-to-image. Other loader-accepted formats (checkpoint, BnB NF4, GGUF, diffusers) are accepted at the family level.
- Required components, resolved by the §11.4 rules (explicit override, then exactly one compatible installed model, otherwise `selection_required` or `missing_component`): `vae` (base `flux`, type `vae`), `t5_encoder` (type `t5_encoder`), and `clip_embed` (type `clip_embed`). The implementing ticket pins the exact base values InvokeAI 6.14.1 records for the T5 and CLIP Embed models. New JSON fields: `components.t5_encoder` and `components.clip_embed`. New flags: `--t5-encoder` and `--clip-embed`. `components.vae` and `--vae` are shared with the other families.
- Defaults: 1024 × 1024, scheduler `euler`, one output. Variant `dev` uses 30 steps and guidance 4.0. Variant `schnell` uses 4 steps and has no guidance. The Execution Receipt omits `resolved_settings.guidance` for `schnell` (user decision, 2026-09-23); it is present for every family and variant where guidance applies.
- Width and height are positive multiples of 16. Steps are positive. Schedulers are `euler`, `heun`, and `lcm`. `guidance` is FLUX distilled guidance: it is finite and at least 1 for `dev`, and supplying it for `schnell` is `invalid_request`. A non-empty negative prompt is `invalid_request` for every FLUX.1 variant. `cfg_scale` is fixed at 1.0 and is not a public field.
- The graph shape is the stock InvokeAI 6.14.1 FLUX.1 text-to-image topology: `flux_model_loader` with explicit T5, CLIP Embed, and VAE identifiers; one positive `flux_text_encoder` whose T5 sequence length comes from the loader's variant-specific output; `flux_denoise`; `core_metadata`; and a non-intermediate `flux_vae_decode` output node. The seed flows through the batch data exactly as in the Anima adapter.
- UI Synchronization: generation Recall sends the same fields as Anima. `ui_sync_partial.not_restored` for FLUX.1 is `scheduler`, `guidance` (dev only), `vae`, `t5_encoder`, `clip_embed`, `output_count`, and `board_id`.

## Standalone `recall`

- The Recall Request Document and flags keep their existing fields. No `guidance` field is added.
- `model` may now be any installed main model of a registered family. Dimensions are validated against that family's alignment and against Recall's 64-pixel minimum. Dimensions and steps still require an explicit model. A FLUX.1 model that is unsupported for generation (`dev_fill`, SDNQ, unknown variant) is `unsupported_capability`. The existing display-name collision rule applies unchanged.

## Starter subfolder sources

- Starter catalog entries (selected starter or returned dependency) whose `source` is `org/repo::path` with a single relative subfolder or file path are supported. The exact catalog string is submitted once to the generic POST installer. The public Installation Request Document, the flags, and the `huggingface` source type are unchanged. A user-supplied `::` reference remains `invalid_request`.
- InvokeAI 6.14.1 checks for a server-local path named by the string before treating it as Hugging Face. No HTTPS URL equivalent exists for a subfolder. The user accepted this residual risk on 2026-09-23: it is recorded, not mitigated.
- Before any mutation, every entry that needs installation passes a shape check (plain repository, non-empty relative path with no leading slash, empty or dot segments, backslash, or `:`), an existence check through Hugging Face's anonymous tree API, and the anonymous public-access check. That API lists folders only, so Bediz lists the source's parent folder and matches the exact path. A `file` entry is sufficient. A `directory` entry must contain at least one file in a recursive listing. InvokeAI's own repository metadata returns `urls: null` for Diffusers-layout repositories, including the FLUX T5, CLIP Embed, and VAE sources, so it cannot establish existence. A missing, denied, malformed, or failed tree response is `unsupported_capability` before mutation. Variant forms and multi-subfolder `+` forms stay `unsupported_capability`.
- A repository that is not verifiably public requires `auth huggingface status: valid` before mutation. InvokeAI then uses its stored login token itself: Bediz sends none and never reads it. `--token-stdin` together with any subfolder entry to install is `unsupported_capability`. InvokeAI writes the stored login token to its temporary install marker while the download runs. The user accepted this on 2026-09-23 as the same class of boundary as ADR 0018. A new ADR records it.

## Capability reporting

- `doctor` registers `generate/sdxl` and `generate/flux` with their endpoint, invocation-schema, and installed-model requirements (SDXL main; FLUX main, FLUX VAE, T5 encoder, CLIP Embed). The FLUX main requirement counts only generation-supported variants and formats, so an inventory with only `dev_fill` or SDNQ FLUX models makes `generate/flux` incompatible. Each carries `ui_sync: partial` only when Recall is also compatible. `ui_sync.generate` stays `partial`.
- If the Recall capability requires `cfg_scale` in the patch schema, that requirement is added to its schema check.

## Live verification baseline and Installation Consent

On 2026-09-23 the user approved installing these models into the local InvokeAI 6.14.1 baseline (RTX 4060, 8 GB VRAM) for live verification:

- SDXL: the `Juggernaut XL v9` starter and its `sdxl-vae-fp16-fix` dependency.
- FLUX.1: the `FLUX.1 schnell (quantized)` and `FLUX.1 dev (quantized)` starters (BnB NF4) and their dependencies: `t5_bnb_int8_quantized_encoder`, `FLUX.1-schnell_ae`, and `clip-vit-large-patch14`.

Install them with `bediz models install --source-type starter`. The FLUX starters and all three of their dependencies use InvokeAI's `repo::subfolder` source form, which ticket 03 enables. On 2026-09-23 the anonymous Hugging Face API reported `InvokeAI/flux_schnell`, `InvokeAI/flux_dev`, `InvokeAI/t5-v1_1-xxl`, and `InvokeAI/clip-vit-large-patch14-text-encoder` as not gated. The FLUX VAE dependency's `black-forest-labs/FLUX.1-schnell` repository is gated (`auto`). The user therefore first logs in to InvokeAI's Hugging Face integration (for example with `! bediz auth huggingface login --token-stdin`) and accepts that repository's terms. The token never enters a ticket, log, or result. FLUX.1 dev weights remain under their non-commercial license. Live verification must not install any other model without new consent.

## Known live risks

- Recall can change the main model in the open UI, and the frontend loads that model asynchronously. It may reset dimensions, scheduler, or VAE after the other patched values are applied. Live browser verification must observe the final UI state.
- On 8 GB VRAM, FLUX.1 generation may fail or be slow. A resource failure is a live-environment limitation to report, not a reason to change the graph contract.
