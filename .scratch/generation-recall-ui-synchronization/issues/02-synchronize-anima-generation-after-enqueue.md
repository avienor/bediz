# 02: Synchronize Anima generation after enqueue

**What to build:** After InvokeAI conclusively accepts an Anima Generation Request, reflect the resolved Recall-supported settings in the open web interface without changing the accepted generation. Patch prompts, exact main model, dimensions, steps, and the first output's Resolved Seed. Keep every resolved setting and seed in the Execution Receipt. A successful patch returns `ui_sync_partial` naming controls that stock 6.14.1 cannot restore; a failed or unsafe patch returns `ui_sync_failed` while generation remains successful. The same behavior applies when `--no-wait` returns immediately and when the CLI waits for completion. Governing sources: V1 §§ 9, 11–12, 17, 22; ADR-0008, ADR-0009, ADR-0017; and the approved feature spec.

**Blocked by:** 01: Submit an Anima Parameter Recall patch.

**Execution route:** `worker + independent review` — enqueue and warning behavior are explicit, can be exercised through public seams, and can be checked independently against the live UI.

**Verification gate:** Public tests prove one enqueue and at most one Recall mutation, Recall only after conclusive enqueue, the first resolved seed for multiple outputs, no generation retry, successful receipts despite Recall failure or ambiguous model name, stable structured warnings, and exactly one JSON result envelope with no progress on stdout. Cover both `--no-wait` and completed generation. On stock 6.14.1, compare visible recalled fields and a completed queue item/image against the Execution Receipt in a browser. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** A separate reviewer checks the fixed base/head diff against V1 and repo standards, independently exercises the Recall-failure success path and one live generation Handoff, and verifies the warning identifies the non-restored controls without implying full synchronization. A repeated enqueue, missing warning, broken JSON envelope, mismatched seed, or misleading UI claim blocks acceptance. After fixes, rerun affected checks and review the changed diff; keep the issue open awaiting independent review until accepted.

**Escalate when:** Recall failure changes the generation outcome; transport uncertainty causes a retry; a multi-output seed cannot be matched to the receipt; live UI state disagrees with the stated partial guarantee; implementation requires changing the Generation Request or Result Envelope contract; or repeated repairs fail for the same cause.

**Permanent records:** V1 spec and tests — V1 §12 and ADR-0017 already record the accepted warning and partial-Handoff behavior; tests must prove it. Revise V1 or supersede ADR-0017 if implementation evidence changes that agreement. `CONTEXT.md` terminology is unchanged.

**Status:** completed

- [x] Both generation modes attempt one supported Recall patch after conclusive enqueue and keep the exact Execution Receipt.
- [x] Successful partial synchronization and failed/unsafe synchronization produce the documented warnings without falsely failing or retrying generation.
- [x] Live browser evidence matches the visible subset and generated output promised by the receipt.
- [x] The verification gate passes and independent review has no acceptance-blocking finding.

## Comments

- Public CLI tests cover one ordered enqueue and Recall patch with the first seed, completed generation, inconclusive enqueue without Recall, Recall HTTP and transport failures, and an unsafe duplicate model display name. Warnings appear in both the Execution Receipt and Result Envelope.
- On local stock InvokeAI 6.14.1, a completed generation returned queue item 25 in batch `7d346fdf-f04f-429e-9b2a-354519df1ce4`, seed 310923, and image `35b60cc3-6aec-4c1c-b5d7-4f45edc775c6.png`. The already-open browser showed the exact prompts, Anima Base 1.0 model, 512×512 dimensions, eight steps, and seed 310923. Its queue panel showed the matching completed batch and seed, and its gallery displayed that image. Scheduler remained Euler and CFG remained 7.5 while the receipt recorded heun and 4.25, matching the partial warning.
- The independent spec reviewer repeated the Recall-failure success tests and live Handoff. Its completed queue item 26 in batch `9d5026c7-25db-4ff2-ab62-b7429df82972`, seed 722639, and image `40acb2fc-44fc-447c-ae72-40ce89e9906f.png` matched the browser queue panel, restored controls, gallery, and receipt. Standards and spec reviews found no acceptance-blocking finding; one inaccurate comment was corrected.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
