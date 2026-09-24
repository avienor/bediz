# 09: Models delete

**What to build:** A human or agent can explicitly remove one installed model. `bediz models delete MODEL_KEY --yes` sends one deletion and returns the summary the model had before deletion (key, name, base, type, format).

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned`. Deleting a managed model removes files, and the difference between managed and in-place registrations must be observed live on 6.14.1 before the contract is accepted.

**Verification gate:** CLI-seam tests cover a missing `--yes` (exit 2, no network request), a name or other non-key selector (resolves nothing, `not_found`), success, a conclusive rejection, and an inconclusive result (`outcome_unknown`). Tests prove that the deletion is sent exactly once and never retried. Live 6.14.1 observations show that deleting a managed model removes its files, while deleting an in-place registration leaves the source file at its original path. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Not required by this route.

**Escalate when:** The live behavior of an in-place deletion removes the source file, or the endpoint deletes dependent models.

**Permanent records:** V1 spec §14 records the observed deletion effects and the exact-key selector. Tests record the contract.

**Status:** ready-for-agent

## Accepted behavior

- The request has `model_key`. It accepts only an exact Model Key, never a display name, because the operation is destructive.
- Bediz reads the model first to build the result summary. An absent key returns `not_found` and sends no mutation.
- `--yes` is execution approval, not a document field. There is no bulk deletion.

- [ ] Deletion requires `--yes`, uses an exact key, and is sent once
- [ ] The live managed and in-place effects are recorded in spec §14
