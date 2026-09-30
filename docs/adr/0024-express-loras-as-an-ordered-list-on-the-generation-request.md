---
status: accepted
---

# Express LoRAs as an ordered list on the Generation Request

This ADR supersedes [ADR-0015](0015-target-invokeai-6-14-and-deliver-v1-in-vertical-slices.md) only for its V1 exclusion of LoRAs from Generation Requests. LoRAs in Generative Upscale remain deferred under [ADR-0012](0012-target-invokeais-generative-upscale-flow.md).

The Generation Request accepts an optional ordered, non-empty list of LoRAs. Each item selects one installed `lora` model by exact Model Key or unique name, with a base matching the main model, and an optional LoRA Weight. The weight uses InvokeAI's web-interface meaning and range, −10 to 10. When omitted, it resolves from the model's `default_settings.weight`, otherwise 0.75. A repeated resolved Model Key is invalid. No LoRA format is excluded. Generation Profiles do not include LoRAs.

Each supported family chains its stock single-LoRA loader in request order. The resulting list and resolved weights go into `core_metadata.loras` and the Execution Receipt. `doctor` has one `setting: "loras"` row per supported family, inheriting that family's text-to-image requirements and adding its LoRA loader and metadata properties. The Generation Mode's requirements and the LoRA requirements are checked together before upload or enqueue.

After a successful generation or upscale enqueue, Automatic Recall attempts to send the exact resolved LoRA list, using an empty list for LoRA-less generation or upscale. A failed or inconclusive enqueue sends no Recall. A display-name collision or incompatible Recall schema prevents the patch and produces `ui_sync_failed`. Overlapping Recall events can complete out of order in stock InvokeAI's frontend; the Execution Receipt remains authoritative.

## Considered alternatives

- The stock `collect` branch was rejected because InvokeAI orders its inputs by random execution node IDs, varying application order.
- A fixed 0.75 default was rejected because InvokeAI stores per-model default weights.
- Sending Recall `loras` only for LoRA generations was rejected because it would leave stale web-interface selections after LoRA-less work.
- LoRAs in profiles were rejected to keep profiles limited to technical defaults.
- Restricting formats was rejected because the stock loader accepts the family's LoRA formats.
