# Generation Recall and UI Synchronization feature spec

**Status:** approved

## Source of truth

This feature implements V1 delivery step 4. The governing sources are V1 §§ 5, 8–12, 17, 22–24; the Bediz glossary definitions of Parameter Recall, UI Synchronization, Synchronization Level, Generation Request, Execution Receipt, and Handoff; ADR-0001, ADR-0003, ADR-0007 through ADR-0009, ADR-0015, and ADR-0017. ADR-0017 supersedes ADR-0016's full generation synchronization claim.

## Accepted baseline

The target is stock InvokeAI 6.14.1. Its Recall endpoint accepts more fields than its frontend applies. For Anima V1, Bediz may claim UI restoration only for positive and negative prompts, the exact main model, dimensions, steps, and seed after these are observed in the live interface. Scheduler, guidance, VAE, Qwen3 encoder, output count, and Output Board remain in the exact Execution Receipt but are not restored to their UI controls. The generation synchronization level is `partial`. The unsupported strict replacement behavior is deferred; `recall --replace` is not a V1 command option.

## Manual Parameter Recall

The schema-version-1 Recall Request Document has the optional fields `model`, `positive_prompt`, `negative_prompt`, `width`, `height`, `steps`, and `seed`, and requires at least one of them. The corresponding convenience flags use the Generation Request spellings: `--model`, `--prompt`, `--negative-prompt`, `--width`, `--height`, `--steps`, and `--seed`. `--request` and operation-field flags do not mix. Unknown or inapplicable fields fail validation. Prompts and seed may be patched alone. Width and height must be supplied together, and dimensions or steps require an explicit Anima main model for family-aware validation. Values use the Anima Generation Request constraints, with the additional stock InvokeAI 6.14.1 Recall API minimum of 64 pixels for each dimension. Smaller Recall dimensions are rejected before posting. A request that the frontend cannot restore exactly is rejected before Recall is posted.

The model selector resolves to one installed Anima main Model Key. Because the stock Recall backend resolves model names by first match among installed main models, Bediz sends the model's display name only if it is unique across that inventory. An ambiguous input name returns `selection_required`; a selected exact key whose display name collides returns `unsupported_capability`. Neither case posts Recall. On API acceptance, `--json` emits one V1 Result Envelope with operation `recall`, data containing `queue_id: "default"` and `mode: "patch"`, and an empty warnings array. The human output says the patch was submitted; the API response does not prove that an open browser received the event. An inconclusive Recall mutation returns `outcome_unknown` and is not retried.

## Automatic synchronization

After a conclusive successful generation enqueue, Bediz attempts one non-strict Recall patch before local waiting or returning `--no-wait`. It uses the exact resolved model and the first output's resolved seed, plus the other supported resolved fields. The accepted generation is never resubmitted because Recall failed. A successful Recall patch still yields the structured `ui_sync_partial` warning listing the resolved generation fields the stock frontend cannot restore. An API failure, inconclusive Recall result, or ambiguous display name yields `ui_sync_failed`; the generation result remains successful with its complete Execution Receipt. No successful result claims full UI restoration.

## Capability and live verification

`doctor` and the Capability Matrix report `partial` generation synchronization only after the implementation and supported-version fixtures exist. Recall requirements are checked separately from graph-producing generation so a missing Recall endpoint does not falsely declare Direct Execution incompatible; `doctor` reports the missing synchronization requirement precisely. The live gate observes the browser's visible controls before and after manual Recall and automatic synchronization, checks that manual Recall did not enqueue a job, and compares the generated queue item, image, metadata, and receipt with the browser view. The browser is verification equipment, not Bediz's control path.

For every worker ticket, implementation evidence leaves the issue awaiting independent review. A separate reviewer starts from a fixed base/head diff, checks spec fidelity and repo standards, independently validates the riskiest evidence, and returns acceptance-blocking findings to implementation. The issue is completed only after the gate passes and the reviewed diff has no acceptance-blocking finding.
