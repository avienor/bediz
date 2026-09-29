# 04: Report trigger phrases and default LoRA weights in `models list`

**What to build:** A Creative Agent can learn, from `models list`, which words activate a LoRA and which weight Bediz will use when the request omits one. Without this, it has to guess from the model description. The feature spec (`.scratch/loras/spec.md`) records the decision.

- **Members:** Each `models list` summary gains two optional members when InvokeAI records them:
  - `trigger_phrases`: an array of strings sorted in ascending order, for any model whose record has a non-empty set;
  - `default_weight`: a number, only for `lora` models whose `default_settings.weight` is set.

  Both are omitted otherwise. Every existing member and filter is unchanged, so `models list --type lora --base sdxl` narrows the list as today.
- **Human output:** Human output shows trigger phrases and the default weight for the models that have them. Lines for models without them are unchanged.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The change adds two optional, read-only result members whose presence and values fake-server tests and one live listing fully describe.

**Verification gate:**
- Public-seam tests against a fake InvokeAI model list prove:
  - sorted `trigger_phrases`, and their omission for an absent, `null`, or empty set;
  - `default_weight` for a `lora` model with a recorded weight, and its omission for an absent weight, an absent `default_settings`, and a non-`lora` model;
  - that existing summaries, filters, and human lines are unchanged for models without these fields;
  - that the JSON output is still exactly one V1 Result Envelope.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live check against the local InvokeAI 6.14.1 baseline: `bediz models list --type lora --json` lists the installed LoRAs. If none carries trigger phrases or a default weight, set them for one LoRA in the InvokeAI web interface's model manager, confirm the members appear, and restore the original values. Report the check as unavailable when no LoRA is installed yet.

**Review gate:**
- A separate reviewer reviews a fixed base/head diff, starting from this ticket, the feature spec, and V1 spec §15.1. The reviewer must be able to read the InvokeAI 6.14.1 model config schemas.
- Before any fix, the reviewer:
  - confirms against the OpenAPI `LoRA_*` configs and `LoraModelDefaultSettings` that the fields are read from their recorded locations;
  - confirms that no other `models list` member changed;
  - reruns the live listing.
- Blocking findings: a changed existing member or human line, nondeterministic ordering, a `default_weight` for a non-`lora` model, or live evidence that cannot be reproduced.

**Escalate when:**
- InvokeAI 6.14.1 records trigger phrases or default weights in a shape the spec does not describe.
- Exposing the members would require a request field or filter change.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec §15.1: the `models list` summary members.
- `CHANGELOG.md` under Unreleased. Tests.
- No ADR or glossary change: the members are additive inventory data.

**Status:** implemented

- [x] `models list` reports sorted `trigger_phrases` and a LoRA's `default_weight` exactly when InvokeAI records them, and nothing else changes.
- [x] The V1 spec and `CHANGELOG.md` record the members.
- [x] The independent review found no acceptance-blocking issue.

## Comments

### Implementation and verification — 2026-09-29

- Implemented the additive inventory members and human output through the existing `models.List` and public CLI seams. Separate red/green cycles covered trigger phrases, recorded LoRA weights, and human output. Fake-server checks cover missing/null/empty metadata, zero and negative weights, non-LoRA omission, unchanged summaries and human lines, and exactly one V1 Result Envelope. Existing request-document and filter checks pass.
- All required commands passed: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. Focused checks passed: `go test ./internal/cli ./internal/models -run '^(TestModelsList|TestList)' -count=1`.
- Live baseline: `curl -fsS --max-time 10 http://127.0.0.1:9090/api/v1/app/version` reported 6.14.1. The live OpenAPI confirms nullable top-level `trigger_phrases` in all 19 `LoRA_*` configs and nullable numeric `LoraModelDefaultSettings.weight` under `default_settings`.
- Built with `go build ./cmd/bediz`. `./bediz models list --type lora --json` listed four installed LoRAs, initially omitting both optional members. In the InvokeAI model manager, temporarily added `bediz-zebra-check` and `bediz-alpha-check` and saved Starting Weight 0.6 on `alienzkin-sdxl` (Model Key `5a64f0b6-e279-4d4e-8c02-826bc2470066`). Repeating the listing returned ascending phrases and `default_weight: 0.6`. `./bediz models list --type lora --base sdxl --json` returned the two SDXL LoRAs; `./bediz models list --type lora --base sdxl` showed both metadata values and preserved the other LoRA's human line.
- Restored the weight and removed both phrases in the UI. Removing the last phrase stores an empty array, so a single `PATCH /api/v2/models/i/5a64f0b6-e279-4d4e-8c02-826bc2470066` with `{"trigger_phrases":null}` restored the original null exactly. A fresh GET confirmed the original null trigger phrases and null weight/min/max settings. The final `./bediz models list --type lora --json` again omitted both members for all four LoRAs.
- Independent Standards and Spec reviewers assessed a fixed implementation snapshot against base `482d8a17b35174d65fccd44edc738b70eba1f6ef`. Both independently read the live model-config schemas and reproduced the listing with the temporary metadata before restoration. The Spec reviewer also compared every existing member across all 20 installed models and checked the SDXL LoRA filter. Standards: 0 findings; Spec: 0 findings; no acceptance-blocking issue.
