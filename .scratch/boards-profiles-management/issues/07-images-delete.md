# 07: Images delete

**What to build:** A human or agent can explicitly delete one gallery image. `bediz images delete IMAGE_NAME --yes` sends one deletion and returns the deleted image name.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The selector is exact and single, the approval is enforced, and the send-once behavior can be tested directly.

**Verification gate:** CLI-seam tests cover a missing `--yes` (exit 2, no network request), success, an absent image (`not_found`), a conclusive rejection, and an inconclusive result (`outcome_unknown`). Tests prove that the deletion is sent exactly once and never retried. The request accepts both flags and a Request Document. `doctor` registers delete only for the tested version range. A live deletion on 6.14.1 is observed in the gallery, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §15, §17, §18, and this ticket. Independently confirm that there is no bulk path, that `--yes` is enforced even with `--request`, and that nothing is retried. Any of those failing blocks acceptance.

**Escalate when:** The 6.14.1 delete endpoint deletes anything beyond the named image.

**Permanent records:** V1 spec §15.1 records the delete contract. Tests record it.

**Status:** ready-for-agent

## Accepted behavior

- The request has `image_name` (exact). There is no name pattern, board-wide, or multi-image selector.
- `--yes` is execution approval, not a document field.
- The result data is `{"image_name":...}`.

- [ ] Deletion requires `--yes` and is sent once
- [ ] Spec §15.1 is updated
