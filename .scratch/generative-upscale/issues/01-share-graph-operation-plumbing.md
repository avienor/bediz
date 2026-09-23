# 01: Share graph-operation plumbing between generation and upscale

**What to build:** Make the generation module's operation-independent parts available to a second graph-producing operation, without changing any observable generation behavior. That covers:

- supported-version checking;
- main-model selection across all installed main models (exact Model Key or unique name, `selection_required` for shared names), separated from operation-specific family acceptance. Today's generation main-model resolution rejects every base outside the generation family registry at selection time. After this ticket, selection returns the installed main model and each operation applies its own family registry: generation still returns the same `unsupported_capability` for an unregistered base, and upscale can accept `sd-1` without registering it as a generation family;
- model inventory reading and exact component resolution (explicit selector, unique name, exactly one compatible model, `selection_required`, `missing_component`);
- complete Model Identifiers and graph primitives (nodes, edges, batch seed data);
- OpenAPI invocation-vocabulary checks;
- the single enqueue with its incomplete-response `outcome_unknown` rule;
- read-only queue waiting with per-item seed and single-image-output verification.

The upscale operation in ticket 02 must be able to reuse these parts through a parser-independent interface instead of copying them. Anima, SDXL, and FLUX.1 generation, Recall, UI Synchronization, and `doctor` stay byte-for-byte the same. This is prefactoring for the `generative-upscale` feature spec. It must not add any upscale types, fields, or behavior.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — a behavior-preserving refactor whose regressions the existing fixtures, public-seam tests, and one live generation reliably detect.

**Verification gate:**
- All existing generation, Recall, synchronization, wait, CLI, and doctor tests pass without changes to their assertions.
- A public test of the shared selection proves that an installed `sd-1` main model is selected by key and by unique name, while `generate` and `recall` with the same model still return `unsupported_capability` with the pre-change message and exit status.
- The Anima, SDXL, and FLUX.1 golden enqueue fixtures are byte-identical.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live against the local InvokeAI 6.14.1 baseline, following the repository's live-verification guide: the live E2E gate (`BEDIZ_E2E_URL` set) passes, and `doctor --json` output is identical to the pre-change binary.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer independently confirms that the generation fixtures are unchanged. The reviewer compares the public JSON and exit status before and after the change for a generation success receipt, a component `selection_required`, a `missing_component`, a `wait_timeout`, and an inconclusive enqueue.
- The reviewer checks that the shared interface has no generation-specific field names, that Cobra stays at the CLI seam, and that no speculative upscale code was added.
- Blocking findings: any observable generation change; any added retry or second mutation; shared code that still assumes a generation Request or output count; main-model selection that still consults the generation family registry.

**Escalate when:** Sharing waiting or resolution requires changing a public generation contract. The shared seam cannot express the upscale rules recorded in the feature spec (Tile ControlNet candidates without automatic selection, a single output) without a redesign. Repeated repair loops fail for the same reason.

**Permanent records:** None — a behavior-preserving refactor. The existing tests remain the contract evidence, and terminology, accepted design, and behavior are unchanged.

**Status:** completed

- [x] Generation, Recall, UI Synchronization, and `doctor` behavior are unchanged, and every generation fixture is byte-identical.
- [x] Main-model selection is shared and independent of any family registry. Generation's family acceptance still rejects unregistered bases exactly as before.
- [x] Version checking, component resolution, graph primitives, single enqueue, and queue waiting can be used by another graph operation without depending on the generation request type.

## Comments

- 2026-09-23: `curl -fsS --max-time 5 http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`.
- 2026-09-23: `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e` passed `TestLiveGate`, including Anima, SDXL, and FLUX.1 direct execution. The gate self-cleaned its uploaded and generated fixture images.
- 2026-09-23: Before/after binaries produced byte-identical `doctor --json` output against the local baseline (`cmp` passed).
- 2026-09-23: After review fixed the shared seed-field seam, the same live E2E command passed again; `doctor --json` remained byte-identical. All required Go tests, race tests, vet, and module verification passed. Independent spec review compared five before/after CLI outcomes byte-for-byte and found no remaining blocking issue.
