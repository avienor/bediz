# 01: Submit an Anima Parameter Recall patch

**What to build:** Let a user submit a schema-version-1 Anima Parameter Recall patch through `bediz recall` without starting generation. Accept the shared Generation Request spellings for positive and negative prompts, exact main model, paired dimensions, steps, and seed through either convenience flags or a Request Document. Recall dimensions must also be at least 64 pixels each, as required by stock InvokeAI 6.14.1. At least one field is required; prompts and seed may stand alone, while dimensions or steps require an explicit Anima model. Resolve an input model to an exact installed Model Key, then use its display name with the stock Recall API only if that name is unique among installed main models. Reject unsupported fields, ambiguous model selection, and unsafe display-name collisions before mutation. A successful result says the API accepted a patch, not that an open browser necessarily received it. Governing sources: V1 §§ 5, 8–12, 17, 22; ADR-0001, ADR-0003, ADR-0007, ADR-0008, ADR-0017; and the approved feature spec.

**Blocked by:** None (can start immediately); the completed Anima Direct Execution slice supplies model resolution and validation conventions.

**Execution route:** `worker + independent review` — the field set and error behavior are fixed, the mutation is bounded, and public CLI tests plus live browser observation can detect false Recall claims.

**Verification gate:** Public CLI tests assert argv, stdout, stderr, exit status, one V1 result envelope, and unchanged queue state for flags and Request Documents. Tests cover partial patches, paired dimensions, family-aware validation, unknown fields, ambiguous selector, duplicate display name, unsupported InvokeAI version/schema, exactly one Recall mutation, and inconclusive transport without retry. A live stock 6.14.1 check observes the supported fields in the open browser after a patch and confirms no queue item was enqueued. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** A separate reviewer examines the fixed base/head diff against V1 and ADR-0017, checks public contract and repo standards, and independently validates the duplicate-name preflight, single-mutation evidence, and one live UI patch. Wrong-model recall, unhandled unsupported fields, accidental enqueue, misleading success wording, or any failed project gate blocks acceptance. After fixes, rerun affected checks and review the changed diff; keep the issue open awaiting independent review until accepted.

**Escalate when:** The stock frontend does not apply a claimed field; a Recall response is successful while model selection is wrong; model-name resolution cannot be made deterministic; the live server differs from its tested OpenAPI contract; a new public field or behavior is required; or two repair attempts fail for the same cause.

**Permanent records:** V1 spec and tests — the accepted behavior is already recorded in V1 §12 and ADR-0017; tests must prove the public contract. Update V1 explicitly if live evidence changes it, and supersede ADR-0017 rather than rewriting it if the accepted design changes. `CONTEXT.md` terminology is unchanged.

**Status:** ready-for-agent

- [ ] `recall` accepts only the supported typed patch fields through flags or a schema-version-1 Request Document, validates them before mutation, and returns the documented success or structured error.
- [ ] An exact model is recalled only when its display name maps back to that same single installed main model; unsafe ambiguity never posts Recall.
- [ ] Live browser evidence shows the accepted patch in the UI while InvokeAI's queue remains unchanged.
- [ ] The verification gate passes and independent review has no acceptance-blocking finding.
