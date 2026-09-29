# 09: Make doctor and command preflight agree

**What to build:** Every InvokeAI requirement that a command checks before a mutation or enqueue appears in a `doctor` capability row, and the command evaluates it with the same Compatibility Check module and the same recorded requirements.

The guarantee is one-directional: when the rows that describe a call are compatible, that call's preflight passes. A row may be stricter than one of its callers when it describes several callers. Each such case is named below and documented in the V1 spec.

**Which rows describe which call:**

| Call | Requirements its preflight evaluates | `doctor` rows that describe it |
| --- | --- | --- |
| `generate` (family, mode) | that family and mode's entry | the `generate` row for that family and mode |
| its UI Synchronization | the Recall common requirements plus that family's additional Recall fields (`cfg_scale` for SDXL) | the `recall` row |
| `upscale` (family) | that family's entry | the `upscale` row for that family |
| its UI Synchronization | the Recall common requirements | the `recall` row |
| standalone `recall` | the Recall common requirements only | the `recall` row, which is stricter (see below) |
| `models install` | the generic install requirements plus those of each source-type row that applies to the request | the generic `models install` row and the applicable source-type rows |
| `auth huggingface login` | the login entry | the `auth.huggingface.login` row |

**Recall.** The `recall` row is recorded as the Recall common requirements plus every family's additional Recall fields. `doctor` keeps reporting it exactly as today, including `incompatible_recall_schema:cfg_scale`, because the row also gates the UI Synchronization levels that V1 §10 defines. It is therefore stricter than standalone `recall`, which does not require `cfg_scale`. Document this asymmetry in V1 §10 and §12. Do not change the row's meaning; that would not be additive. Standalone `recall` keeps accepting installations whose Recall schema lacks `cfg_scale`, as the existing successful Recall fixture shows.

**New `models install` rows.** Following the `starter` precedent, `doctor` gains three rows:

- `family: "huggingface"`: the generic install requirements plus `GET /api/v2/models/hugging_face`. It applies to `huggingface` sources.
- `family: "path"`: the generic install requirements plus the `inplace` query parameter. It applies to `path` sources.
- `family: "source_token"`: the generic install requirements plus the `access_token` query parameter. It applies whenever a Source Token is supplied.

The generic `models install` row keeps its current requirements. New failure values follow the existing vocabulary: `missing_endpoint:GET /api/v2/models/hugging_face`, `incompatible_install_schema:inplace`, and `incompatible_install_schema:access_token`.

**Text-to-image and upscale endpoints.** Text-to-image `generate` and `upscale` preflight also check their entries' endpoints, as image-to-image already does. A missing endpoint is `unsupported_capability`, with the message image-to-image uses today.

**Before implementing,** inventory every command-side InvokeAI requirement. If any requirement is missing from the table above, or cannot be expressed as an additive row or entry requirement, escalate.

This is decision C of `.scratch/architecture-deepening/spec.md`. The new rows and failure values are additive under V1 spec §25. Source of truth: V1 spec §5, §10, §12, §14.1, §14.3, §14.5, §14.7, and §14.8, ADR-0008, ADR-0017, and ADR-0022.

**Blocked by:** 08 (Evaluate capability entries with one Compatibility Check module).

**Execution route:** `frontier-owned`. It changes `doctor`'s public report and command preflight, and it requires precise spec text for new capability rows, a documented Recall asymmetry, and live verification against InvokeAI.

**Verification gate:**
- New `doctor` fixture tests:
  - on the stock InvokeAI 6.14 fixture, the three new rows are compatible;
  - on variants missing each new requirement, only the affected row is incompatible, and it names the new failure value.
- New command tests:
  - text-to-image `generate` and `upscale` return `unsupported_capability` before any upload or enqueue when an entry endpoint is missing;
  - standalone `recall` still succeeds on a Recall schema without `cfg_scale`, while `doctor` still reports the `recall` row incompatible with `incompatible_recall_schema:cfg_scale`.
- A test or table asserts, for each call in the table above, that every requirement its preflight evaluates belongs to the requirements of the `doctor` rows named for it.
- **Existing tests:**
  - Assertions about unrelated behavior pass unmodified.
  - Assertions that enumerate `doctor` rows, such as the exact operation list, may change only to add the three accepted rows. Every pre-existing row must keep its content and relative order on existing fixtures, including its operation, family, mode, compatibility, failures, and UI Synchronization level. A test proves this.
- V1 spec §10, §12, and §14 (§14.1, §14.3, §14.5, §14.7) document the new rows, the stricter text-to-image and upscale preflight, and the Recall asymmetry.
- `CHANGELOG.md` records the change under `Unreleased`.
- The agent skill is checked for any enumeration of `doctor` rows and updated if it has one.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- **Live check:** read `docs/agents/live-verification.md`, then run `doctor --json` against the supported local InvokeAI 6.14.x baseline and confirm that the three new rows are compatible and that no pre-existing row changed. Record the result, or record that the baseline was unavailable.

**Review gate:** Not required by this route. A standards and spec review of the diff is still recommended because `doctor` output is a public contract.

**Escalate when:**
- A command-side requirement is missing from the table or cannot be expressed additively.
- Keeping a pre-existing row's content or order is impossible.
- The live baseline reports a new row, or any pre-existing row, as incompatible.
- Adding endpoint checks to text-to-image or upscale would reject the stock baseline.
- The agent skill relies on a row shape the change would alter.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec §10, §12, and §14: the new `doctor` rows, the stricter preflight, and the documented Recall asymmetry.
- `CHANGELOG.md` under `Unreleased`.
- The agent skill, if it enumerates `doctor` rows.
- Public tests.

**Status:** implemented

- [x] Recall, model install, and Hugging Face login preflight run through the Compatibility Check module with the requirements named in the table.
- [x] `doctor` reports the `huggingface`, `path`, and `source_token` install rows with the new failure values, and every pre-existing row keeps its content and order.
- [x] Text-to-image generate and upscale preflight check their entries' endpoints.
- [x] Standalone `recall` is unchanged, and the stricter `recall` row is documented.
- [x] A test proves every preflight requirement belongs to the requirements of the `doctor` rows named for its call.
- [x] The V1 spec and `CHANGELOG.md` are updated, the live `doctor` check is recorded, and all verification commands pass.

## Comments

- 2026-09-29 live check: `go run ./cmd/bediz doctor --url http://127.0.0.1:9090 --json` against InvokeAI 6.14.1 returned `ok: true` with zero issues. The new `models.install` rows `huggingface`, `path`, and `source_token` were each compatible with empty failures. Compared with the same command from the pre-change HEAD, all 31 pre-existing capability rows were identical in content and order.
- 2026-09-29 verification: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
