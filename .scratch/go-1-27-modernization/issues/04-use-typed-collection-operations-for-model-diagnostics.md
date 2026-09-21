# 04: Use typed collection operations for deterministic model diagnostics

**What to build:** Apply the Modern Go Guidelines `slices_sort_func`, `cmp_or`, and `slices_contains` rules to installed-model results and readiness diagnostics. `models list` must remain sorted by model name and then model key. Relevant diagnostic models must retain their existing type-then-name ordering, capability matching must retain exact string membership semantics, and no new tie-breaker becomes part of public output.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — typed collection APIs make the refactor mechanically bounded, while output ordering is externally visible and merits an independent contract check.

**Verification gate:** Exact-order fixtures prove name-then-key ordering for installed models and type-then-name ordering for diagnostic models, including equal primary fields. Capability readiness results remain unchanged. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Escalate when:** The implementation would add or remove an ordering key, change ascending order, alter capability membership semantics, or reveal that current diagnostic ordering is intentionally unspecified. Any requested change to the `models list` order is a V1 public-contract decision.

**Status:** done

- [x] Installed-model sorting uses a typed comparator and preserves the V1 name-then-key order exactly.
- [x] Diagnostic relevant-model sorting uses a typed comparator and preserves type-then-name order without adding a new tie-breaker.
- [x] Exact string membership uses the standard slice operation with unchanged matching behavior.
- [x] Focused ordering/readiness tests and all repository verification commands pass.

## Comments

Change (uncommitted diff against `7cba5de`):

- `internal/models/models.go` — `sort.Slice` closure replaced by `slices.SortFunc(result.Models, func(a, b Summary) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Key, b.Key)) })`. Same two keys, same ascending order, no third key; `sort` import dropped for `cmp` and `slices`.
- `internal/doctor/doctor.go` — `inspectModels` uses `slices.SortFunc(relevant, func(a, b ModelSummary) int { return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Name, b.Name)) })`; the private `contains` helper is deleted and `modelMatches` calls `slices.Contains` for both `requirement.Types` and `requirement.Bases`. `slices.Contains` uses `==`, so membership stays exact — no prefix or case folding.
- Both `slices.SortFunc` and the previous `sort.Slice` are unstable; element pairs equal on both keys keep unspecified relative order, exactly as before, so no new tie-breaker is observable.

Fixture work at the public seams (`models.List`, `doctor.Run`):

- `TestListSortsModelsByNameThenKey` now feeds `z-earlier/Earlier`, `a-later/Later`, `same-b/Same`, `same-a/Same` and asserts the full key order `[z-earlier a-later same-a same-b]`. The previous three-row fixture could not distinguish name-primary from key-primary ordering because both orders agreed on it; the new row pair makes name-primary dominance observable while `same-a`/`same-b` still cover the equal-primary-field case.
- New `TestRunOrdersRelevantModelsByTypeThenName` serves six models in an order that differs from the sorted result and asserts `Relevant` exactly: two `main` models ordered Alpha/Zeta, then `qwen3_encoder`, then two `vae` models ordered Alpha VAE/Zeta VAE. Equal primary fields (two `main`, two `vae`) and the type primary key are both exercised. The fixture also carries a near-miss model `{key: near-miss, name: Anima V2, base: anima-v2, type: main}` that must be excluded from `Relevant` and must not raise `Anima main model` availability to 3 — this is what pins exact membership.
- Readiness is asserted alongside ordering: every requirement in the fixture is satisfied, per-requirement availability is `Anima main model: 2`, `Anima-compatible VAE: 2`, `Qwen3 text encoder: 1`, and `generate` plus `models.list` stay compatible. Availability is checked by requirement name rather than by comparing the whole `Requirements` slice, so a future capability-matrix addition does not fail an ordering test.

Mutation evidence (throwaway in-place mutation, reverted): with the models comparator swapped to key-then-name, `TestListSortsModelsByNameThenKey` fails with `model order = [a-later same-a same-b z-earlier], want [z-earlier a-later same-a same-b]`; with the diagnostic comparator swapped to name-then-type, `TestRunOrdersRelevantModelsByTypeThenName` fails with `relevant models = [main/Alpha vae/Alpha VAE qwen3_encoder/Qwen3 main/Zeta vae/Zeta VAE]` against the type-then-name expectation. Both fixtures therefore fail on the plausible regression each one defends.

Live verification against the supported local InvokeAI baseline (6.14.1 at `127.0.0.1:9090`): `bediz models list --json` returned five models whose `[name, key]` sequence equals its own sort (independent `jq` sort comparison, `sorted: true`); `bediz doctor --json` exited 0 with `ready: true`, `Relevant` ordered `main/Anima Base 1.0`, `qwen3_encoder/Anima Qwen3 0.6B Text Encoder`, `vae/Anima QwenImage VAE` (type-then-name, `sorted: true`), and all three model requirements satisfied at their required counts — readiness unchanged from before the refactor.

Verification (Go 1.27, module `go 1.27.0`, fresh `-count=1` runs):

```text
go test -count=1 ./...        ok (all packages)
go test -race -count=1 ./...  ok (all packages)
go vet ./...                  clean
go mod verify                 all modules verified
```

Independent review (two axes, uncommitted diff):

- Spec axis: all four checklist items implemented; ordering keys and direction unchanged; `slices.Contains` reproduces the deleted helper's `==` semantics; the fixture set covers equal primary fields, proves genuine sorting (pre-sort append order differs from the asserted output), and pins exact membership through the `anima-v2` near-miss. No scope creep: the `models_test` fixture strengthening is required to prove name-primary dominance, and `sort.Strings` in `internal/queue/queue.go` belongs to ticket 05. No escalation trigger fires — the diagnostic order stays as implemented, not redefined.
- Standards axis: no documented-standard violations and no Modern Go guideline applicable to the touched hunks left unapplied. Three P3 judgement calls: (1) the initial exact `[]ModelRequirement` comparison would have failed on future capability-matrix growth — acted on, now name-keyed availability; (2) new test fixtures keep `encoding/json` v1 — kept, per the `json_v2` guideline's own scope note and file consistency; (3) the repeated `httptest` switch scaffold matches the four existing fixtures in `doctor_test.go` — kept, pre-existing convention.
