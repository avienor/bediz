# LoRA generation

**Status:** Approved ticket plan (2026-09-29). Tickets 01–06 are ready for agent in dependency order.

This feature lets `generate` apply LoRAs for the registered generation families Anima, SDXL, and FLUX.1, in both Generation Modes. V1 spec §2 and ADR-0015 left LoRAs out of V1. After image-to-image, this is the second creative operation taken from that list. A LoRA is an installed InvokeAI model of type `lora`. It is selected like any other installed model and applied with a weight. It changes neither the Generation Mode nor the Result Envelope operation.

The accepted product boundary is the V1 specification, in particular §8–12, §14, §15.1, §16, §18, and §25. Together with it: ADR-0003 (Contract Parity), ADR-0006 (Request Documents), ADR-0008 (tested versions and unsafe retries), ADR-0009 (Execution Receipts), ADR-0011 (typed, family-aware core generation), ADR-0013 (local technical profiles), ADR-0014 (Creative Discretion in the skill), ADR-0017 (verified Recall only), ADR-0022 (schema version 1 stays additive), and ADR-0023 (image-to-image). Every change here is additive under schema version 1. Each implementing ticket moves the behavior it delivers into the V1 specification. A contradiction with the V1 specification or an ADR is an escalation, not permission to silently change the contract.

## Decisions

Recorded with the user on 2026-09-29.

### Public shape

- The Generation Request gains one optional member, `loras`: an ordered, non-empty array of objects `{"model": "<Model Key or unique name>", "weight": <number>}`. `weight` is optional. Each element follows the §8.1 strict object rules: an unknown member, an explicit `null`, or a repeated member name is `invalid_request`.
- An empty `loras` array is `invalid_request`; a request without LoRAs omits the member.
- The flag is `--lora <selector>[=<weight>]`. It is repeatable and keeps the order of its occurrences. It is a string-array flag, so commas are never split. The value is split at its last `=`. When an `=` is present, the suffix must parse as a finite decimal number; otherwise the result is `invalid_request`. An empty selector is `invalid_request`. A name containing `=` needs its Model Key or a Request Document. `--lora` is an operation flag and cannot be combined with `--request`.
- `weight` is the LoRA Weight, with InvokeAI's web-interface meaning. It must be finite and satisfy `−10 ≤ weight ≤ 10`, the bounds of the stock 6.14.1 web-interface number input and of the Recall API's `LoRARecallParameter.weight`. Zero and negative values are valid.
- An omitted weight resolves to the LoRA's InvokeAI default weight (`default_settings.weight`) when its model record has one, and otherwise to 0.75, the stock web-interface default. The stock frontend applies the same rule when a person adds a LoRA. A recorded default outside the bounds above is `invalid_request` naming that element's `weight`, and its message asks for an explicit weight.
- `invalid_request` names the Request Document path in `error.details.field` (§9): `loras` for list-level errors, `loras.<index>.model` and `loras.<index>.weight` for element errors. Flags report the same paths, with the index of the flag occurrence.
- Bediz sets no maximum number of LoRAs.
- LoRAs apply to text-to-image and image-to-image alike. A request without `loras` behaves exactly as today.
- Generation Profiles gain no `loras` member. A profile's `loras` remains an unknown field.

### Resolution and validation order

1. **Before any network request:** the `loras` shape, the non-empty rule, flag parsing, and explicit weight bounds, alongside every existing local check.
2. **After main-model resolution, before any upload or enqueue:**
   - The family must have a registered LoRA capability; otherwise `unsupported_capability`. Until its family ticket lands, a family without LoRA support rejects every request with `loras` this way.
   - Each selector resolves by exact Model Key or unique name, following the component-override rules (§11.4). An ambiguous name is `selection_required` with kind `lora` and candidates sorted by Model Key. An unknown selector is `invalid_request` naming `loras.<index>.model`.
   - A resolved model whose type is not `lora`, or whose base differs from the main model's base, is `unsupported_capability`. Bediz does not restrict LoRA formats. InvokeAI's collection loaders accept every `lora` format of the family's base.
   - Two elements that resolve to the same Model Key are `invalid_request` naming `loras`. InvokeAI 6.14.1's single-LoRA loaders fail at execution time on a repeated key, and its collection loaders silently skip one. Bediz rejects it before enqueue instead.
   - Omitted weights resolve as described above.
   - The live LoRA invocation vocabulary is checked together with the Generation Mode's requirements. A missing schema or property is `unsupported_capability` naming it.
3. **Enqueue:** the existing single, never-retried `enqueue_batch`.

A request without `loras` does not check the LoRA vocabulary.

### Graphs (InvokeAI 6.14.1)

Each family keeps its tested graph for the Generation Mode and chains InvokeAI's stock single-LoRA loader invocation once per LoRA, in request order. The user chose this on 2026-09-29 so that the application order is deterministic.

- **Chain:** Each loader is intermediate and carries the LoRA's complete Model Identifier as `lora` and its resolved `weight`. The first loader takes the model loader's outputs. Each later loader takes the previous loader's outputs. The last loader's outputs feed the consumers that the model loader fed before.
- **SDXL:** `sdxl_lora_loader` passes `unet`, `clip`, and `clip2`. The last loader's `unet` feeds `denoise_latents.unet`, and its `clip` and `clip2` feed both `sdxl_compel_prompt` nodes. The VAE path, including a `vae_loader` override, is unchanged.
- **Anima:** `anima_lora_loader` passes `transformer` and `qwen3_encoder`. The last loader's `transformer` feeds `anima_denoise.transformer`, and its `qwen3_encoder` feeds both the positive and the negative `anima_text_encoder`. The VAE path is unchanged.
- **FLUX.1:** `flux_lora_loader` passes `transformer`, `clip`, and `t5_encoder`. The last loader's `transformer` feeds `flux_denoise.transformer`, and its `clip` and `t5_encoder` feed `flux_text_encoder`. The `max_seq_len` and VAE edges are unchanged. Dev and schnell are both supported.
- **Why not the stock branch:** The stock frontend instead collects `lora_selector` outputs with `collect` into a collection loader. InvokeAI 6.14.1 orders `collect` items by randomly generated execution node IDs, so that branch applies the LoRAs in an order that varies between runs. LoRA patches are summed into weights in reduced precision, so the order can change output pixels. The chain applies them in request order on every run. The metadata matches the stock frontend's.
- **Metadata:** `core_metadata.loras` is `[{"model": <Model Identifier>, "weight": <resolved weight>}]` in request order. It is omitted without LoRAs.
- Without LoRAs, every graph is unchanged. Existing fixtures stay byte-for-byte identical.
- Waiting is unchanged. The LoRA nodes produce no images.

### Execution Receipt

- `submitted_request.loras` retains the submitted list.
- `resolved_settings.loras` is `[{"model_key": "...", "weight": <resolved weight>}]` in request order. It is omitted without LoRAs, so existing receipts are unchanged.

### Capability reporting

- `doctor` gains one row per family with LoRA support, for example `{"operation":"generate","family":"sdxl","setting":"loras","compatible":true,"failures":[]}`. A LoRA row has a `setting` member and no `mode` member. Existing rows are unchanged and have no `setting` member. LoRA rows carry no `ui_sync` member.
- A LoRA row requires the family's text-to-image requirements plus the input properties below. Each schema also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties. The properties were confirmed against the live InvokeAI 6.14.1 OpenAPI on 2026-09-29.

| Schema (type) | Families | Required input properties |
| --- | --- | --- |
| `SDXLLoRALoaderInvocation` (`sdxl_lora_loader`) | SDXL | `lora`, `weight`, `unet`, `clip`, `clip2` |
| `AnimaLoRALoaderInvocation` (`anima_lora_loader`) | Anima | `lora`, `weight`, `transformer`, `qwen3_encoder` |
| `FluxLoRALoaderInvocation` (`flux_lora_loader`) | FLUX.1 | `lora`, `weight`, `transformer`, `clip`, `t5_encoder` |
| `CoreMetadataInvocation` (`core_metadata`) | all | text-to-image properties plus `loras` |

- A LoRA row's model requirements are the family's text-to-image model requirements. It requires no installed LoRA, so `doctor`'s `ready` does not depend on LoRAs being installed. The live E2E gate is a separate matter: ticket 01 makes it require the SDXL LoRA that the live-verification guide records.
- A failure's reach depends on which requirement it names:
  - A missing family loader schema or property makes only that family's LoRA row incompatible.
  - A missing `core_metadata.loras` makes every LoRA row incompatible, and every mode row stays compatible.
  - A missing requirement that a LoRA row inherits from its family's text-to-image requirements fails the mode rows that require it too, exactly as today.
- A compatible LoRA row and a compatible mode row together imply that a LoRA request in that mode passes InvokeAI compatibility preflight.
- Human `doctor` output names LoRA rows distinctly and keeps existing lines unchanged.

### UI Synchronization

- **Until ticket 05 lands**, automatic Recall is unchanged. A generation with LoRAs appends `loras` to `ui_sync_partial.not_restored`.
- **After ticket 05:**
  - **Generation:** After every successful generation enqueue, automatic Recall sends `loras`. The value is the exact resolved list in request order as `{"model_name": <display name>, "weight": <resolved weight>}`, or `[]` when the generation used no LoRAs. The stock 6.14.1 frontend replaces its LoRA list with the recalled list, and an empty list clears it. After a Recall that no other Recall overlaps, the web interface therefore shows only the LoRAs the latest execution used. It also drops LoRAs the person had selected there.
  - **Overlapping Recalls:** The stock frontend clears the list at once but adds each recalled LoRA only after fetching its model config. A later Recall can therefore arrive while an earlier Recall's fetch is still pending, for example after two quick `--no-wait` runs. The earlier LoRA is then added after the later clear, and the web interface can keep it. The main model already has the same limitation, because the frontend applies it asynchronously too. Bediz cannot order the browser's work, and it does not manipulate browser state (V1 spec §12). This limitation is documented, not prevented. The Execution Receipt remains the authoritative record.
  - **Upscale:** After every successful upscale enqueue, the upscale Recall patch sends `loras: []`. The stock Upscale tab applies the web interface's LoRA list, but Bediz upscale uses none.
  - **Display-name rule:** InvokeAI 6.14.1 resolves each Recall LoRA by display name across every installed `lora` model, whatever its base, and takes the first match. Bediz therefore sends a LoRA's display name only when it identifies that single installed LoRA. Otherwise it sends no Recall patch and adds `ui_sync_failed`, as it does for a main-model name collision. Generation and upscale outcomes, their single enqueue, and their receipts are unchanged.
  - **Compatibility:** The `recall` doctor row, generation synchronization, and upscale synchronization additionally require `RecallParameter.loras`. It must be a nullable array whose items reference `LoRARecallParameter` with `model_name` (string) and `weight` (number). When it is missing, the result is `incompatible_recall_schema:loras`, `ui_sync_failed`, and omitted `ui_sync` members.
  - **Unchanged:** Standalone `recall` does not require or accept `loras`.
  - **Warnings:** `ui_sync_partial.not_restored` no longer names `loras`.

### Model inventory

`models list` summaries gain two optional members when InvokeAI records them:

- `trigger_phrases`: sorted, for any model whose record has a non-empty set.
- `default_weight`: only for `lora` models whose `default_settings.weight` is set.

Both are omitted otherwise.

### Terminology

`CONTEXT.md` gains the following entries; ticket 01 finalizes their wording:

- **LoRA:** a low-rank adaptation model that InvokeAI applies on top of a compatible main model during generation. It is selected by Model Key or unique name, and its base must match the main model's Model Family. _Avoid:_ style preset, embedding, Control LoRA.
- **LoRA Weight:** the strength with which one LoRA is applied, with InvokeAI's web-interface meaning. 0 removes its effect, and negative values invert it. An omitted weight resolves to the LoRA's InvokeAI default weight, otherwise 0.75. _Avoid:_ strength, Denoising Strength, scale.

## Evidence from InvokeAI 6.14.1

These findings come from the installed 6.14.1 package, the backend invocations and graph execution service, and the built frontend bundle. The live-verification guide says where they are. In the bundle, search for `sdxl_lora_collection_loader`, `anima_lora_collection_loader`, `flux_lora_collection_loader`, `recall_parameters_updated`, `loraRecalled`, `loraAllDeleted`, and `numberInputMin:-10`.

- **Graph builders.** For each base, the frontend keeps the enabled LoRAs whose base equals the base being built. It adds one `collect` node, the base's collection loader, and one `lora_selector` per LoRA, then reroutes the model loader's outputs through the collection loader. It writes `loras: [{model, weight}]` into metadata. The stock Upscale graph applies the same SDXL LoRA branch.
- **`collect` order.** The graph execution service builds a `collect` collection from its item edges, sorted by each source's iteration path and then by its execution node ID. Execution node IDs are random UUIDs. Non-iterated `lora_selector` nodes share an empty iteration path, so their order varies between runs. An independent review observed the reversed order in 13 of 20 runs.
- **Loaders.** The single-LoRA loaders `sdxl_lora_loader`, `anima_lora_loader`, and `flux_lora_loader` take `lora` and `weight` (no bounds, default 0.75). Each appends its LoRA to the lists it passes through, and each raises an error when the key is already applied. The Anima and FLUX collection loaders raise an error for a LoRA of another base, and every collection loader silently skips a repeated key.
- **Weights.** The frontend LoRA weight constants are `initial: 0.75`, slider −1 to 2, number input −10 to 10. Adding a LoRA uses the model's `default_settings.weight` when set. The model record also carries `default_settings.weight_min` and `weight_max`, and `trigger_phrases`.
- **Model records.** LoRA configs exist for base `sdxl` (LyCORIS, Diffusers, OMI), `flux` (LyCORIS, Diffusers, OMI), and `anima` (LyCORIS). Control LoRAs have their own type (`control_lora`) and are not `lora` models.
- **FLUX.1 quantized models.** `flux_denoise` applies LoRAs as sidecar layers when the transformer is quantized, including BnB NF4.
- **Recall.** `RecallParameter.loras` is a nullable array of `LoRARecallParameter`: `model_name` (required), `weight` (−10 to 10, default 0.75), and `is_enabled` (default true). The backend resolves each name to the first installed `lora` model with that name and skips unknown names. When `loras` is an array, the frontend `recall_parameters_updated` handler clears the LoRA list synchronously (`loraAllDeleted`). It then fetches each LoRA's config and adds it with the recalled weight in the promise callback (`loraRecalled`). An empty array therefore clears the list. The main model is applied in a promise callback as well. The order in which the two finish is not guaranteed, and a pending callback from an earlier event can run after a later event's clear. An independent review reproduced this offline with the stock handler and a delayed config response.
- **Metadata.** `CoreMetadataInvocation.loras` is a nullable array of `LoRAMetadataField` with required `model` and `weight`.

## Live verification baseline

On 2026-09-29 the local InvokeAI 6.14.1 baseline had the main models and components for all three families (see the live-verification guide), and no LoRA.

**Installation Consent.** On 2026-09-29 the user granted Installation Consent for the LoRA downloads this feature's live verification needs. The preferred models are:

| Family | Source | Size | License | Notes |
| --- | --- | --- | --- | --- |
| SDXL | Starter "Alien Style", `https://huggingface.co/RalFinger/alien-style-lora-sdxl/resolve/main/alienzkin-sdxl.safetensors` | 228 MB | "other", per its model card | Trigger `alienzkin` |
| SDXL | Starter "Noodles Style", `https://huggingface.co/RalFinger/noodles-lora-sdxl/resolve/main/noodlez-sdxl.safetensors` | 228 MB | "other", per its model card | Trigger `noodlez` |
| Anima | `https://huggingface.co/LyliaEngine/Anima_Detail_Tweaker/resolve/main/Anima_Detail_Tweaker.safetensors` | 92 MB | CDLA-Permissive-2.0 | Ungated |
| FLUX.1 | `https://huggingface.co/XLabs-AI/flux-RealismLora/resolve/main/lora.safetensors` | 22 MB | FLUX.1 [dev] Non-Commercial License | Ungated |

- A preferred model may be unavailable, or InvokeAI may not record it as `type: lora` with the expected base. In that case any ungated LoRA of the same base up to 500 MB may replace it. Record its source, size, and license in the ticket comments. When the replaced model is the SDXL LoRA that the live E2E gate uses, the same change updates the gate and the live-verification guide to the substitute.
- Install through Bediz (`models install`), using the starter source for the SDXL entries. The installed LoRAs stay on the baseline, and the live-verification guide lists them.
- This consent covers development verification only. It does not change the product's Installation Consent rule (V1 spec §18) or the agent skill's.
- Follow the live-verification guide: 768 × 768 and one output unless the behavior under test needs more. Compare outputs at a fixed seed with and without the LoRA to show its effect. List every ad hoc image so the user can remove it.

## Known live risks

- FLUX.1 LoRAs on the BnB NF4 transformer run as sidecar layers and can be slow or exceed 8 GB VRAM. The int8 T5 encoder with a LoRA that targets T5 is untested. A resource failure is a live-environment limit to report, not a graph defect.
- InvokeAI may identify a downloaded LoRA under an unexpected base or type.
- Recall applies the main model and the LoRAs asynchronously in the web interface, and a model change can remove LoRAs of another base. Record the final observed state after model loading.
- Overlapping Recalls can leave an earlier execution's LoRA in the web interface. This is documented, not prevented.
- A LoRA's effect can be subtle at low weights. Use a weight of at least 0.75 and a fixed seed for comparisons.

## Out of scope

- LoRAs in generative upscale graphs. Upscale only clears the web interface's LoRA list after ticket 05.
- LoRAs in Generation Profiles.
- `loras` in standalone `recall`.
- Disabled LoRAs (`is_enabled: false`), per-LoRA step ranges, and separate text-encoder weights.
- Control LoRAs, IP adapters, FLUX Redux and Kontext reference images, and ControlNet generation.
- LoRAs for SD1.5 or other unregistered families.
- Adding trigger phrases to prompts. Prompts remain the Creative Agent's.
- Restricting LoRA formats.
- Live verification of InvokeAI 6.14.2, and a Bediz release. Changes accumulate under `Unreleased` in `CHANGELOG.md`.

## Revisions

- **2026-09-29, after plan review:**
  - The graphs chain single-LoRA loaders instead of the stock `collect` branch, a user decision. InvokeAI's `collect` order varies between runs, and the chain applies LoRAs in request order.
  - The `doctor` failure reach is split into family loader, shared `core_metadata.loras`, and inherited text-to-image requirements.
  - The web-interface guarantee is limited to Recalls that do not overlap, and the overlap limitation is documented.
  - `ready` stays independent of LoRAs, while the live E2E gate requires the SDXL LoRA the guide records; a substitute updates both.
  - The skill adds trigger phrases only to prompts the agent writes, never to a prompt the user fixed.
  - `models list` is specified in V1 spec §15.1, not §14.

## Tickets

1. `issues/01-sdxl-lora-generation.md`: Apply LoRAs to SDXL generation. Blocked by nothing.
2. `issues/02-anima-lora-generation.md`: Apply LoRAs to Anima generation. Blocked by 01.
3. `issues/03-flux1-lora-generation.md`: Apply LoRAs to FLUX.1 generation. Blocked by 01.
4. `issues/04-models-list-lora-metadata.md`: Report trigger phrases and default LoRA weights in `models list`. Blocked by nothing.
5. `issues/05-ui-sync-restores-loras.md`: Restore the exact LoRA list through UI Synchronization. Blocked by 01, 02, and 03.
6. `issues/06-skill-and-readme-loras.md`: Teach the agent skill and README LoRAs. Blocked by 01–05.
