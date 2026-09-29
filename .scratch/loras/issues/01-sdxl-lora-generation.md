# 01: Apply LoRAs to SDXL generation

**What to build:** `generate` applies one or more LoRAs to SDXL generation, in both text-to-image and image-to-image, when the request lists them. This ticket establishes the public LoRA contract that tickets 02 and 03 extend to Anima and FLUX.1. The feature spec (`.scratch/loras/spec.md`) records every decision. The essentials follow.

- **Request:** The Generation Request gains an optional `loras` member: an ordered, non-empty array of `{"model": <Model Key or unique name>, "weight": <number>}`, where `weight` is optional.
  - **Flag:** `--lora <selector>[=<weight>]`, repeatable, order preserved, never split on commas, and split at the last `=`. A non-numeric weight suffix or an empty selector is `invalid_request`. `--lora` cannot be combined with `--request`.
  - **Weight bounds:** A weight must be finite with `−10 ≤ weight ≤ 10`.
  - **Weight default:** An omitted weight resolves to the LoRA's InvokeAI `default_settings.weight`, otherwise 0.75. A recorded default outside the bounds is `invalid_request` asking for an explicit weight.
  - **Local errors:** An empty array, an explicit `null`, or an unknown element member is `invalid_request`. Errors name `loras`, `loras.<index>.model`, or `loras.<index>.weight` in `error.details.field`, for flags as well as documents.
  - **Profiles:** Generation Profiles gain nothing, and a request without `loras` behaves exactly as today.
- **Resolution:** After main-model resolution, and before any upload or enqueue:
  - Each selector resolves by exact Model Key or unique name under the component-override rules. An ambiguous name is `selection_required` with kind `lora`, and an unknown one is `invalid_request`.
  - A non-`lora` model, or a LoRA whose base is not `sdxl`, is `unsupported_capability`. No format is excluded.
  - Two elements that resolve to the same Model Key are `invalid_request` naming `loras`.
  - An Anima or FLUX.1 main model with `loras` is `unsupported_capability` until ticket 02 or 03 lands, before any upload or enqueue.
- **Graph:** The tested SDXL graph for the Generation Mode, plus:
  - one intermediate `sdxl_lora_loader` per LoRA, chained in request order, each with the complete Model Identifier as `lora` and the resolved `weight`;
  - the first loader taking `sdxl_model_loader`'s `unet`, `clip`, and `clip2`, and each later loader taking the previous loader's outputs;
  - the last loader's `unet` feeding `denoise_latents`, and its `clip` and `clip2` feeding both `sdxl_compel_prompt` nodes;
  - `core_metadata.loras` set to `[{model, weight}]` in request order.

  The chain replaces the stock frontend's `collect` branch, a user decision recorded in the feature spec. InvokeAI orders `collect` items by random node IDs, so the stock branch's application order varies between runs, while the chain applies LoRAs in request order. The VAE path, including a `vae_loader` override, and the image-to-image encoder path are unchanged. Graphs without LoRAs, and their fixtures, are byte-for-byte unchanged.
- **Execution Receipt:** `submitted_request.loras` is kept as submitted. `resolved_settings.loras` is `[{model_key, weight}]` in request order, with resolved weights. Both are omitted without LoRAs.
- **Doctor:** A new row `{"operation":"generate","family":"sdxl","setting":"loras",...}` has no `mode` and no `ui_sync` member. It requires the SDXL text-to-image requirements plus:
  - `SDXLLoRALoaderInvocation` (`sdxl_lora_loader`) with `lora`, `weight`, `unet`, `clip`, and `clip2`;
  - `CoreMetadataInvocation` with its text-to-image properties plus `loras`.

  Each schema also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties. The row has no LoRA model requirement.
- **Preflight:** A request with `loras` checks the Generation Mode row's requirements plus these additional requirements before any upload or enqueue. A missing schema or property is `unsupported_capability` naming it.
- **Failure reach:**
  - A missing `sdxl_lora_loader` schema or property makes only the SDXL LoRA row incompatible.
  - A missing `core_metadata.loras` makes the LoRA rows incompatible and leaves every mode row compatible.
  - A missing inherited text-to-image requirement fails the mode rows that require it too, exactly as today.

  Existing rows, their compatibility, and existing human output lines are unchanged. Human output names the LoRA row distinctly.
- **UI Synchronization:** Automatic Recall is unchanged in this ticket. A generation with LoRAs appends `loras` to `ui_sync_partial.not_restored`. Ticket 05 replaces this interim behavior.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned`. This slice fixes a new public contract, adds a new kind of `doctor` row, records an ADR and glossary terms, and needs live judgment about the LoRA's visible effect.

**Verification gate:**
- Versioned InvokeAI 6.14.1 SDXL LoRA enqueue fixtures are checked against the live OpenAPI invocation vocabulary. They cover one LoRA, two LoRAs whose chain order equals request order, a LoRA with a VAE override, and a LoRA with image-to-image.
- Public-seam tests prove:
  - the local validation matrix: empty list, `null`, unknown member, weight bounds and non-finite weight, flag parsing (last `=`, non-numeric suffix, empty selector, a comma kept in the selector), flag and document parity, `--lora` with `--request`, and `error.details.field` paths;
  - that none of these local failures makes a network request;
  - resolution by key and by unique name, `selection_required` with kind `lora`, an unknown selector, wrong type, wrong base, and duplicates;
  - weight resolution from an explicit value, from a recorded default, and from the 0.75 fallback, and an out-of-bounds recorded default;
  - that Anima and FLUX.1 LoRA requests are `unsupported_capability` before any upload or enqueue;
  - that a missing `sdxl_lora_loader` schema or property fails a SDXL LoRA `generate` before any upload or enqueue, leaves a LoRA-less `generate` unaffected, and marks only the SDXL LoRA row incompatible with the property named;
  - that a missing `core_metadata.loras` fails LoRA `generate` and marks the LoRA rows incompatible while every mode row stays compatible;
  - that an inherited text-to-image requirement still fails the mode rows that require it, together with the LoRA row;
  - one enqueue, never retried;
  - the receipt members, including unchanged LoRA-less receipts and fixtures;
  - the interim `not_restored` addition.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live checks against the local InvokeAI 6.14.1 baseline, following the live-verification guide, with `Juggernaut-XL-v9`:
  - Install the consented starter LoRAs "Alien Style" and "Noodles Style" through `bediz models install`. The Installation Consent is recorded in the feature spec. Confirm `models list --type lora` reports them with base `sdxl`.
  - A waited 768 × 768 text-to-image with `alienzkin` in the prompt and "Alien Style" at 1.0, and the same request without the LoRA at the same seed. The outputs differ visibly, and the LoRA output's metadata shows `loras` with the model and weight.
  - A two-LoRA request whose receipt and metadata keep request order, and a request whose omitted weight resolves as specified.
  - An image-to-image request with a LoRA.
  - `doctor --json` reports the SDXL LoRA row as compatible, and every existing row is unchanged.
  - The live E2E gate (`BEDIZ_E2E_URL` set) passes and gains a self-cleaning SDXL LoRA generation case. The case uses the SDXL LoRA recorded in the live-verification guide: the "Alien Style" starter, or the substitute recorded under the feature spec's consent. When that LoRA is absent, the case fails with a message naming the source to install. `doctor`'s `ready` still does not depend on any LoRA.
- Report every live step that was unavailable. List ad hoc images.

**Review gate:** Not required by this route.

**Escalate when:**
- InvokeAI 6.14.1 rejects the recorded chain, or `sdxl_lora_loader` needs inputs the spec does not record.- A LoRA at weight 1.0 has no visible effect at a fixed seed.
- InvokeAI records a consented LoRA under another base or type, and no substitute within the consent can be installed.
- Representing LoRA rows in `doctor` would change an existing row's compatibility, an existing human output line, or `ready`.
- The weight default rule cannot be applied from the model inventory response.
- Repeated repair loops fail.

**Permanent records:**
- New ADR-0024, "Express LoRAs as an ordered list on the Generation Request". It records these decisions:
  - LoRAs extend `generate` and are selected like component overrides, with the base matching the main model.
  - Each family chains its stock single-LoRA loader in request order, so the application order is deterministic.
  - The weight has web-interface meaning and bounds, and its default comes from the model's recorded weight, then 0.75.
  - Duplicates are rejected.
  - `doctor` has one `setting: "loras"` row per family.
  - Automatic Recall always sends the exact LoRA list: an empty list for LoRA-less generations and upscale, and `ui_sync_failed` on a display-name collision. Overlapping Recalls are a documented stock-frontend limitation.
  - Profiles are excluded.

  It lists these considered and rejected alternatives:
  - the stock `collect` branch, rejected because its application order varies between runs;
  - a fixed 0.75 default;
  - sending Recall `loras` only for LoRA generations;
  - LoRAs in profiles;
  - restricting formats.
- `CONTEXT.md`: add **LoRA** and **LoRA Weight**, and relate them to the Generation Request.
- V1 spec:
  - §2: LoRAs become an outcome and leave the exclusion list.
  - §9: the `loras` field paths.
  - §10: the `setting` member, the SDXL LoRA row, and its preflight.
  - §11: `loras`, `--lora`, and a new LoRA subsection recording shape, resolution, weights, the graph with the SDXL row, metadata, and receipt.
  - §12: the interim `not_restored` addition.
- `CHANGELOG.md` under Unreleased. Tests.
- The live-verification guide: the installed LoRAs and the SDXL LoRA the gate requires.

**Status:** ready-for-agent

- [ ] `generate` with an SDXL main model and `loras` applies them through one tested enqueue in both Generation Modes, with the recorded graph, metadata, and receipt.
- [ ] Every local and resolution failure is reported before any upload or enqueue, and LoRA-less generation, its fixtures, and its receipts are unchanged.
- [ ] Anima and FLUX.1 LoRA requests fail as `unsupported_capability` before any upload or enqueue.
- [ ] `doctor`, ADR-0024, `CONTEXT.md`, the V1 spec, the live-verification guide, and `CHANGELOG.md` reflect the behavior verified live, and every unavailable live step is reported.
