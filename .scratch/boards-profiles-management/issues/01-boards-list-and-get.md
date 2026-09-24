# 01: Boards list and get

**What to build:** An agent can discover Output Boards before choosing a `board_id`. `bediz boards list` returns a page of normalized board summaries. `bediz boards get SELECTOR` returns one board by exact identifier or unique name. Both work with flags or a schema-version-1 Request Document and emit one V1 Result Envelope with `--json`.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The behavior is read-only and fully specified below, and fixture-backed CLI tests can detect a wrong implementation.

**Verification gate:** CLI-seam tests cover the list defaults, the limit bounds (exit 2 before the network), a lookup by exact id, a lookup by unique name, an ambiguous name (`selection_required`, exit 3, candidates sorted by board id), and an absent board (`not_found`). Tests also show that a response carrying extra backend fields does not leak them into the output. `doctor` registers both operations as read-only inspection. A live `boards list` and `boards get` run against InvokeAI 6.14.1, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed base/head diff against spec §8.2, §15, and this ticket. Independently check the ambiguous-name path, the candidate ordering, and the output allowlist. A leaked backend field, an implicit first-match choice, or a mismatch with the 6.14.1 endpoint shape blocks acceptance.

**Escalate when:** The 6.14.1 boards API cannot page the way described (for example, because it only offers an all-boards mode), or it does not expose the fields needed for the summary.

**Permanent records:** V1 spec §15 gains the board list and get contract and the board summary fields. Tests record it.

**Status:** ready-for-agent

## Accepted behavior

- `boards list` accepts `offset` (default 0), `limit` (default 20, valid 1–100), and `include_archived` (default false). Results are newest first, and ties are broken by board id. The result also carries the total count that InvokeAI reports.
- A board summary contains only `board_id`, `board_name`, `image_count`, `archived`, an optional `cover_image_name`, and the created and updated timestamps.
- `boards get SELECTOR` first tries an exact board identifier. If nothing matches, it searches every board, including archived ones, for an exact name that is case-sensitive. A single match wins. Several matches return `selection_required` with `kind: "board"`, and each candidate carries `board_id` and `board_name`. No match returns `not_found`.
- Read-only inspection may run on an untested but compatible InvokeAI version (§5).
- The expected 6.14.1 endpoints are the boards list and board detail routes under `/api/v1/boards/`. Confirm them against the live OpenAPI document before coding.

- [ ] `boards list` works with flags and with a Request Document
- [ ] `boards get` resolves an exact id and a unique name, and returns `selection_required` or `not_found` otherwise
- [ ] Board output is allowlisted and normalized
- [ ] `doctor` reports both operations
- [ ] Spec §15 is updated
