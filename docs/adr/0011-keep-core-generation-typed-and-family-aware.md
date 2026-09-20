---
status: accepted
---

# Keep core generation typed and family-aware

The canonical generation request will represent prompts as a required positive prompt and an optional negative prompt. A non-empty negative prompt will be rejected when the selected model family does not support that behavior; it will never be discarded silently.

Canonical requests will express image size as exact `width` and `height` values. The two fields must be supplied together or both omitted so family or profile defaults can resolve them. Bediz will validate the resolved dimensions against the selected family's supported bounds and alignment. Aspect-ratio presets are a UI convenience and will not be part of the V1 request contract.

Core settings such as steps, scheduler, and guidance may be optional typed request fields. Bediz will apply them only for model families where their semantics are supported and tested. Supplying an inapplicable field will produce a validation error. V1 will not provide an arbitrary advanced key-value escape hatch.

## Consequences

The same request field has a documented and testable meaning instead of being passed optimistically into whichever graph is selected. Agents must omit settings that do not apply to a model family, and extending generation behavior requires an intentional schema and capability-matrix update.
