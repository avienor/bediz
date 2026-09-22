---
status: accepted
---

# Report only verified Recall behavior on stock InvokeAI

This supersedes ADR-0016's full generation synchronization claim. On stock InvokeAI 6.14.1, the Recall API accepts more fields than the frontend applies: its event handler maps prompts, main model, dimensions, steps, and seed to UI state, but not Anima scheduler, guidance, VAE, Qwen3 encoder, output count, or Output Board controls. Its strict mode sends null for omitted scalar fields, which the frontend ignores. Bediz therefore reports partial generation synchronization and does not expose `recall --replace` in V1.

Bediz continues to use the InvokeAI API for Recall and the browser only for live verification. The generation request and Execution Receipt retain exact resolved settings even when the open UI cannot show every control. A later supported InvokeAI version can earn full synchronization or replacement behavior after both its API and frontend are tested.

## Consequences

The controller can deliver useful Parameter Recall and Handoff on the stock baseline without depending on an upstream patch or browser automation. Capability reporting and warnings describe the fields that the installed frontend cannot restore.
