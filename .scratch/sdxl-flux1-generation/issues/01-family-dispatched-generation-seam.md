# 01: Dispatch generation, Recall, and capability checks through Model Family adapters

**What to build:** Replace the Anima-only generation path with a Model Family seam, without changing any observable Anima behavior except validation timing. A Generation Request's `model` selector (Model Key or unique name) resolves against all installed main models. The resolved model's base picks a registered family adapter. The adapter owns defaults, family-specific validation, component applicability and resolution, graph compilation, the invocation-schema requirements checked before enqueue, the Execution Receipt's `component_keys`, and the UI Synchronization patch and `not_restored` list. Only Anima is registered in this ticket, so a main model of any other base returns `unsupported_capability` as it does today. Standalone `recall` resolves its `model` through the same family registry, so a family registered later is accepted there automatically, subject to that family's dimension alignment and the 64-pixel Recall minimum. Family-independent checks still run before any network request: schema version, required model and positive prompt, width and height supplied together, and unknown fields. Family-specific checks (dimension alignment, scheduler, guidance, negative-prompt and component applicability) run after main-model resolution and before the enqueue mutation, and still fail as `invalid_request` with exit status 2. A component selector the resolved family does not use is `invalid_request`. The single enqueue, the no-retry rule, waiting, the Resolved Seed Set ordering, the Execution Receipt shape, and warning codes are unchanged. Feature decisions are recorded in the `sdxl-flux1-generation` feature spec.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — the accepted behavior is fully specified, the change is reversible, and the existing Anima tests plus the byte-identical graph fixture and a live Anima run detect regressions.

**Verification gate:**
- The existing Anima compiler, resolution, wait, CLI, recall, synchronization, and doctor tests pass. The Anima golden enqueue fixture is byte-identical.
- The only permitted test changes are those that assert family-specific validation before connection. Each is updated to show that the same `invalid_request` and exit status 2 now occur after inventory resolution and before any enqueue.
- New public-seam tests cover:
  - a main-model selector resolving to an unregistered base (`sdxl`, `flux`, `sd-1`) → `unsupported_capability` with no enqueue;
  - a name shared across families → `selection_required` with candidates sorted by Model Key;
  - an unknown component field → `invalid_request` before network. The recognized but inapplicable selector test belongs to ticket 04, when those fields and a second family exist;
  - `recall` with a non-Anima main model → `unsupported_capability` before mutation.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- One live Anima `generate` (waited, one output) against the local InvokeAI 6.14.1 baseline produces the same receipt shape and `ui_sync_partial` warning as before. `doctor --json` reports the same capability set.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer independently confirms that the Anima enqueue fixture is unchanged. The reviewer diffs the public JSON of an Anima success receipt, a `selection_required` result, and a `missing_component` result before and after the change.
- The reviewer checks that no family-specific logic remains in the CLI adapter, that Cobra stays at the seam, and that no speculative SDXL or FLUX code was added.
- Blocking findings: any Anima graph, receipt, warning, exit-status, or error-code change other than the documented validation timing; any added retry or second mutation; family knowledge leaking into CLI or parser code.

**Escalate when:** Preserving Anima behavior needs a public-contract change beyond validation timing. The seam cannot express the SDXL optional-VAE or FLUX variant-dependent rules recorded in the feature spec without a redesign. Live Anima behavior differs from the pre-change baseline. Repeated repair loops fail for the same underlying reason.

**Permanent records:** V1 spec and tests. §11.2 records that family-specific validation happens after main-model resolution, and that the main selector resolves across registered families. §12 records that Recall accepts the main models of registered families. No new terminology or ADR: the Model Family is already defined, and ADR 0011 already requires family-aware validation.

**Status:** implemented

- [x] Anima generation, Recall, and doctor output are unchanged apart from the documented validation timing, and the Anima graph fixture is byte-identical.
- [x] Main models of unregistered families fail with `unsupported_capability`, and unknown component fields fail with `invalid_request`, before any mutation. Ticket 04 covers recognized but inapplicable component selectors.
- [x] Adding a family requires only registering an adapter and its capability entry, not changing generate, Recall, or CLI control flow.

## Comments

- 2026-09-23: `curl -fsS http://127.0.0.1:9090/api/v1/app/version` reported InvokeAI 6.14.1. `go run ./cmd/bediz models list --json` showed the installed Anima main, VAE, and Qwen3 encoder, plus SDXL models.
- 2026-09-23: The current and baseline binaries produced byte-identical `doctor --json` output on the local baseline. With the same mock InvokeAI inventory and explicit seed, their public JSON and exit statuses matched for an Anima success receipt, cross-family `selection_required`, and `missing_component`.
- 2026-09-23: `/tmp/bediz-baseline-YCRTjio1/bediz-current generate --model 'Anima Base 1.0' --prompt 'a small lighthouse in a storm, illustration' --width 768 --height 768 --seed 42023 --output-count 1 --json` completed with one output, receipt keys `submitted_request`, `resolved_settings`, `queue`, `outputs`, and `warnings`, and `ui_sync_partial` with the existing `not_restored` fields. The gallery image is `4894db05-2d5a-4166-8cde-4c5bd7467b7d.png` (queue item 33, batch `8bde5f9b-51d5-4318-be08-42cfb92f37bf`).
- 2026-09-23: The only registered family is Anima and both currently recognized component selectors apply to it. The requested public-seam test for a recognized but inapplicable selector cannot execute until a second family or a new selector is registered. `components.t5_encoder` remains an unknown field rejected before network as required by the current V1 contract. Ticket 04 owns the new T5 and CLIP Embed fields and its inapplicability tests.
- 2026-09-23: User decision: keep `t5_encoder` and `clip_embed` fields and the recognized inapplicable-selector test in ticket 04. Ticket 01 is complete under that scope.
