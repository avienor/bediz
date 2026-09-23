# 04: Generate FLUX.1 dev and schnell images with UI Synchronization and Recall

**What to build:** Register the FLUX.1 family adapter so that `generate` accepts an installed main model with base `flux`, type `main`, and InvokeAI variant `dev` or `schnell`, in the stock 6.14.1 loader-accepted non-SDNQ formats `checkpoint`, `bnb_quantized_nf4b`, or `gguf_quantized`. FLUX.1 Krea dev and Kontext dev report variant `dev` and are accepted for text-to-image when their format is supported. Variant `dev_fill`, a missing or unknown variant, SDNQ, ordinary `diffusers`, and other formats return `unsupported_capability` before enqueue.

- **Components:** Three required components are resolved by the V1 §11.4 rules: explicit override, then exactly one compatible installed model, otherwise `selection_required` or `missing_component` with install guidance. They are `vae` (base `flux`, type `vae`), `t5_encoder` (type `t5_encoder`), and `clip_embed` (type `clip_embed`). Pin the exact base values that InvokeAI 6.14.1 records for T5 and CLIP Embed models.
- **New fields and flags:** `components.t5_encoder` / `--t5-encoder` and `components.clip_embed` / `--clip-embed`. A `qwen3_encoder` selector is `invalid_request`.
- **Defaults:** 1024 × 1024, scheduler `euler`, one output. Variant `dev` uses 30 steps and guidance 4.0. Variant `schnell` uses 4 steps and has no guidance.
- **Validation:** Width and height are positive multiples of 16. Steps are positive. Schedulers are `euler`, `heun`, and `lcm`. `guidance` is FLUX distilled guidance: finite and at least 1 for `dev`, and `invalid_request` when supplied for `schnell`. A non-empty negative prompt is `invalid_request` for every variant. `cfg_scale` is fixed at 1.0 and is not public.
- **Graph:** The stock InvokeAI 6.14.1 FLUX.1 text-to-image topology: `flux_model_loader` with explicit T5, CLIP Embed, and VAE identifiers; one positive `flux_text_encoder` whose T5 sequence length comes from the loader's variant-specific output; `flux_denoise`; `core_metadata`; and a non-intermediate `flux_vae_decode` output node carrying the optional board. The batch seed data keeps `item_ids` and `resolved_settings.seeds` aligned by position.
- **Receipt:** `component_keys` contains `vae`, `t5_encoder`, and `clip_embed`. `resolved_settings.guidance` is present only when guidance applies. For `schnell` the field is omitted, never `0` or `null`. Anima, SDXL, and FLUX.1 dev receipts are unchanged.
- **Automatic UI Synchronization:** Recall patches prompts, model, dimensions, steps, and the first seed. It emits `ui_sync_partial` with `not_restored` set to `scheduler`, `guidance` (dev only), `vae`, `t5_encoder`, `clip_embed`, `output_count`, and `board_id`.
- **Standalone `recall`:** It accepts supported FLUX.1 main models, with 16-pixel alignment and the 64-pixel minimum. It rejects unsupported variants and formats as `unsupported_capability`.
- **Doctor:** `doctor` registers `generate/flux` with its endpoints, invocation schemas, and installed main, VAE, T5, and CLIP Embed requirements. It carries `ui_sync: partial` only when Recall is compatible. The main-model requirement counts only models that generation would accept (variant `dev` or `schnell`, format other than `sdnq_quantized`). Today's doctor model requirements match only base and type, so they gain a variant and format filter.
- **Terminology:** Add the Model Variant concept to the glossary.

**Blocked by:** 01: Dispatch generation, Recall, and capability checks through Model Family adapters; 03: Install starter entries that use Hugging Face subfolder sources (the live baseline is installed through that path).

**Execution route:** `frontier-owned` — the slice introduces variant-dependent applicability and defaults, a new public term, three required components, and a resource-heavy live baseline. Those involve judgment about live evidence that checks alone cannot settle.

**Verification gate:**
- Versioned FLUX.1 enqueue fixtures for InvokeAI 6.14.1 cover `dev` and `schnell`, with and without a board. They are checked against the live OpenAPI invocation vocabulary.
- Public-seam tests prove:
  - variant-specific defaults;
  - `dev_fill`, unknown variant, and SDNQ rejection;
  - guidance rejected for `schnell`, and negative prompts rejected for both variants;
  - scheduler and 16-pixel alignment bounds;
  - T5, CLIP Embed, and VAE resolution, ambiguity, and missing-component results;
  - a recognized but inapplicable `qwen3_encoder` selector → `invalid_request` after FLUX main-model resolution and before any enqueue, as deferred from ticket 01;
  - flag and Request Document parity for the new component fields;
  - seed ordering, a single enqueue, no retry, and `outcome_unknown`;
  - the FLUX `not_restored` list and Recall-failure warning;
  - `recall` acceptance and rejection;
  - `doctor` matching, including an inventory whose only FLUX main models are `dev_fill` or SDNQ, which makes `generate/flux` incompatible;
  - a `schnell` receipt without `guidance` and a `dev` receipt with it.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live against the local InvokeAI 6.14.1 baseline, following the repository's live-verification guide, after installing the consented `FLUX.1 schnell (quantized)` and `FLUX.1 dev (quantized)` starters and their T5 int8, FLUX VAE, and CLIP Embed dependencies (install them with `bediz models install --source-type starter`, which ticket 03 enables; the FLUX VAE dependency lives in a gated repository, so the user first provides a valid InvokeAI Hugging Face login and accepts that repository's terms):
  - one waited `schnell` generation and one waited two-output `dev` generation with an explicit seed produce receipts whose images carry the matching seeds;
  - `doctor --json` reports `generate/flux` as compatible with `ui_sync: partial`;
  - using the `browser-harness-local` skill, a browser records the UI state after Recall and model loading;
  - the live E2E gate (`BEDIZ_E2E_URL` set) passes with `generate/flux` registered and gains a self-cleaning FLUX.1 schnell generation case.
- Report any live step that was unavailable, including 8 GB VRAM limits.

**Review gate:** Not required by this route.

**Escalate when:**
- InvokeAI 6.14.1 records a FLUX variant, component base, or format differently from the feature spec.
- The stock FLUX text-to-image graph needs inputs the spec does not record, such as true CFG, DyPE, or a max-sequence-length override.
- Kontext or Krea models behave differently from `dev` in text-to-image.
- The frontend resets recalled values after loading a FLUX model.
- A download needs a credential path outside ADR 0018.
- The baseline hardware cannot complete a generation and the failure looks like a graph defect rather than a resource limit.
- Repeated repair loops fail.

**Permanent records:**
- `CONTEXT.md`: add **Model Variant**, the InvokeAI-recorded subtype of a Model Family (such as FLUX.1 dev or schnell) that can change applicable settings and defaults.
- V1 spec: §2 and §11.1–11.6 record the FLUX.1 fields, variant rules, defaults, bounds, components, graph contract, and receipt `component_keys`. §12 records the FLUX Recall fields and the `not_restored` list. §10 records the `generate/flux` doctor entry. §11.4 records that FLUX components are resolved while the loader's SDNQ self-contained fallback stays unsupported.
- Tests.
- No new ADR: ADR 0011 already governs family-aware applicability.

**Status:** implemented

- [x] `generate` produces FLUX.1 dev and schnell images through one tested enqueue, with variant-specific defaults and applicability exactly as recorded.
- [x] T5 encoder, CLIP Embed, and VAE resolution never picks an arbitrary model, and unsupported variants or formats fail before mutation.
- [x] The Execution Receipt, `ui_sync_partial` warning, standalone `recall`, and `doctor` reflect FLUX.1 behavior verified live, and any live step that was unavailable is reported.

## Comments

2026-09-23 live verification on InvokeAI 6.14.1 at `http://127.0.0.1:9090`:

- The consented FLUX.1 dev and schnell BnB NF4 models and their T5 int8, FLUX VAE, and CLIP Embed dependencies were already installed. No install job or credential action was needed.
- `/tmp/bediz-flux-verify doctor --url http://127.0.0.1:9090 --json` reported `generate/flux` compatible with `ui_sync: partial`, two supported main models, and all three required components.
- `/tmp/bediz-flux-verify generate --model 385ce753-8fb8-46fc-ab74-aac4924a9663 --prompt 'a small red lighthouse by a calm sea, clear daylight' --width 768 --height 768 --seed 424201 --url http://127.0.0.1:9090 --json` completed queue item 47 and image `776dc0b7-56c3-42ac-95ca-368387d56fb0.png`. The receipt omitted guidance and associated seed 424201 with the image.
- `/tmp/bediz-flux-verify generate --model 6b6e6ba3-368f-42ee-b6e5-033bba37aa7b --prompt 'a small blue sailboat on a calm sea, clear daylight' --width 768 --height 768 --seed 424301 --output-count 2 --url http://127.0.0.1:9090 --json` completed item IDs `[49, 48]` with seeds `[424301, 424302]` and images `58326e54-0137-47ba-8565-d12e11784e5f.png` and `55578bc9-5f0a-466b-ab14-6caf9cb3b013.png` in the same order. Guidance was 4.0.
- Using `browser-harness-local` on the open InvokeAI tab after Recall and model loading, dev showed the sailboat prompt, 768 × 768, 30 steps, and seed 424301. The subsequent schnell E2E Recall showed the teacup prompt, 768 × 768, 4 steps, and seed 44. Scheduler and guidance were not claimed as restored by Recall; the receipt named their applicable limitations.
- `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test ./e2e -run '^TestLiveGate$' -count=1 -v` passed. Its FLUX.1 schnell case generated and then removed `8421d39e-6373-4b53-a0cd-64f292d1bf71.png`. The three ad hoc images above remain in the gallery for the user to remove. The 8 GB GPU completed every generation.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
- Review found that the original parent spec counted ordinary FLUX `diffusers` as supported, while stock InvokeAI 6.14.1's `FluxModelLoaderInvocation.invoke` accepts checkpoint-based configs or `Main_SDNQ_Diffusers_FLUX_Config` and rejects `Main_Diffusers_FLUX_Config`. After escalation and continuation, the accepted 6.14.1 format set was pinned to `checkpoint`, `bnb_quantized_nf4b`, and `gguf_quantized` in the parent spec, V1 spec, generation preflight, doctor, and tests.
- The same `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test ./e2e -run '^TestLiveGate$' -count=1 -v` gate passed again after the format correction. Its FLUX.1 schnell case generated and removed `6ee98c62-a481-4e9e-b652-9da9cb390711.png`.
