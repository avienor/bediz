# 07: Images delete

**What to build:** A human or agent can explicitly delete one gallery image. `bediz images delete IMAGE_NAME --yes` sends one deletion and returns the deleted image name.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The selector is exact and single, the approval is enforced, and the send-once behavior can be tested directly.

**Verification gate:** CLI-seam tests cover a missing `--yes` (exit 2, no network request), success, an absent image (`not_found`), a conclusive rejection, and an inconclusive result (`outcome_unknown`). Tests prove that the deletion is sent exactly once and never retried. The request accepts both flags and a Request Document. `doctor` registers delete only for the tested version range. A live deletion on 6.14.1 is observed in the gallery, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §15, §17, §18, and this ticket. Independently confirm that there is no bulk path, that `--yes` is enforced even with `--request`, and that nothing is retried. Any of those failing blocks acceptance.

**Escalate when:** The 6.14.1 delete endpoint deletes anything beyond the named image.

**Permanent records:** V1 spec §15.1 records the delete contract. Tests record it.

**Status:** implemented

## Accepted behavior

- The request has `image_name` (exact). There is no name pattern, board-wide, or multi-image selector.
- `--yes` is execution approval, not a document field.
- The result data is `{"image_name":...}`.

- [x] Deletion requires `--yes` and is sent once
- [x] Spec §15.1 is updated

## Comments

- Live InvokeAI 6.14.1: `/tmp/bediz-images-delete-live images upload /tmp/bediz-delete-live-3b39d1e1-ef74-4423-bb99-9b99c8318bf2.png --url http://127.0.0.1:9090 --json` created `5ac7241c-d49a-4ef5-9ccb-976a1947e638.png`. After refreshing the Generate gallery, its Uncategorized count changed from `31 | 4` to `31 | 5`.
- `/tmp/bediz-images-delete-live images delete 5ac7241c-d49a-4ef5-9ccb-976a1947e638.png --yes --url http://127.0.0.1:9090 --json` returned `{"image_name":"5ac7241c-d49a-4ef5-9ccb-976a1947e638.png"}`. `/tmp/bediz-images-delete-live images get 5ac7241c-d49a-4ef5-9ccb-976a1947e638.png --url http://127.0.0.1:9090 --json` returned `not_found` (exit 6); after refreshing the gallery, the count returned to `31 | 4`. The temporary local PNG and binary were removed.
- Independent review found that URL path normalization could turn a path-like name such as `../uncategorized` into InvokeAI's bulk delete route before the response was checked. The request now rejects path separators, percent escapes, and complete `.` or `..` names locally; CLI tests assert no network request for these inputs.
- Review also found that the shared JSON transport accepted a valid first object followed by junk or another JSON value. It now requires exactly one JSON value, so a malformed successful delete response is `outcome_unknown` without another send.
