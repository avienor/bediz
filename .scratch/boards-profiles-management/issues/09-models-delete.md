# 09: Models delete

**What to build:** A human or agent can explicitly remove one installed model. `bediz models delete MODEL_KEY --yes` sends one deletion and returns the summary the model had before deletion (key, name, base, type, format).

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned`. Deleting a managed model removes files, and the difference between managed and in-place registrations must be observed live on 6.14.1 before the contract is accepted.

**Verification gate:** CLI-seam tests cover a missing `--yes` (exit 2, no network request), a name or other non-key selector (resolves nothing, `not_found`), success, a conclusive rejection, and an inconclusive result (`outcome_unknown`). Tests prove that the deletion is sent exactly once and never retried. Live 6.14.1 observations show that deleting a managed model removes its files, while deleting an in-place registration leaves the source file at its original path. The live check uses only small throwaway models installed for it: one managed install and one in-place registration from a temporary server path. It never touches a baseline or user model. Afterward, the model inventory must match the inventory before the check, apart from the throwaway source file, which is then removed by hand. Follow `docs/agents/live-verification.md`. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Not required by this route.

**Escalate when:** The live behavior of an in-place deletion removes the source file, the endpoint deletes dependent models, or no suitably small throwaway model is available.

**Permanent records:** V1 spec §14 records the observed deletion effects and the exact-key selector. Tests record the contract.

**Status:** done

## Accepted behavior

- The request has `model_key`. It accepts only an exact Model Key, never a display name, because the operation is destructive.
- Bediz reads the model first to build the result summary. An absent key returns `not_found` and sends no mutation.
- `--yes` is execution approval, not a document field. There is no bulk deletion.

- [x] Deletion requires `--yes`, uses an exact key, and is sent once
- [x] The live managed and in-place effects are recorded in spec §14

## Comments

- 2026-09-24, live verification on InvokeAI 6.14.1 (`http://127.0.0.1:9090`) with two 16 MB throwaway copies of the installed Anima LLLite Sketch file, named `bediz-delete-inplace` and `bediz-delete-managed`, in a temporary directory. `models install --source-type path` registered the first in place (key `8b2b5939-a4af-44d2-baf0-fb46b7220c56`); `--move --yes` moved the second into managed storage (key `54736837-727c-4a21-8d7c-4a951e200560`). Both jobs completed.
- `models delete bediz-delete-managed --yes --json` (a name) returned `not_found`, exit 6, with no deletion. `models delete 54736837-… --json` without `--yes` returned `invalid_request`, exit 2.
- `models delete 54736837-… --yes --json` returned the pre-deletion summary (`anima`, `controlnet`, `checkpoint`); the managed key directory and its file were gone afterwards. `models delete 8b2b5939-… --yes --json` returned its summary, and the source file stayed at its temporary path. Repeating the in-place deletion returned `not_found`.
- The `models list --json` inventory after the check was identical to the inventory before it (16 models), so no dependent or other model was deleted. The throwaway source file was then removed by hand. The baseline Anima LLLite Sketch file was only read. `doctor --json` reported `models.delete` compatible.
- CLI-seam tests cover missing `--yes` and other invalid input without network traffic, name and case-changed selectors (`not_found`, no deletion), Request Documents, success, a record for another key or with a missing summary field, conclusive rejections (404, 409, 500), and inconclusive results (gateway status, lost response), each with exactly one deletion. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
