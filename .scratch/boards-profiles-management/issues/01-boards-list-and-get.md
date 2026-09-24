# 01: Boards list and get

**What to build:** An agent can discover Output Boards before choosing a `board_id`. `bediz boards list` returns a page of normalized board summaries. `bediz boards get SELECTOR` returns one board by exact identifier or unique name. Both work with flags or a schema-version-1 Request Document and emit one V1 Result Envelope with `--json`.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The behavior is read-only and fully specified below, and fixture-backed CLI tests can detect a wrong implementation.

**Verification gate:** CLI-seam tests cover the list defaults, the limit bounds (exit 2 before the network), a lookup by exact id, a lookup by unique name, an ambiguous name (`selection_required`, exit 3, candidates sorted by board id), and an absent board (`not_found`). Tests also show that a response carrying extra backend fields does not leak them into the output. `doctor` registers both operations as read-only inspection. A live `boards list` and `boards get` run against InvokeAI 6.14.1, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed base/head diff against spec §8.2, §15, and this ticket. Independently check the ambiguous-name path, the candidate ordering, and the output allowlist. A leaked backend field, an implicit first-match choice, or a mismatch with the 6.14.1 endpoint shape blocks acceptance.

**Escalate when:** The 6.14.1 boards API cannot page the way described (for example, because it only offers an all-boards mode), or it does not expose the fields needed for the summary. Also escalate when its visibility scope differs from the per-user scope described below.

**Permanent records:** V1 spec §15 gains the board list and get contract and the board summary fields. Tests record it.

**Status:** done

## Accepted behavior

- `boards list` accepts `offset` (default 0), `limit` (default 20, valid 1–100), and `include_archived` (default false). Results keep the order InvokeAI returns: newest first by creation time. InvokeAI 6.14.1 applies no tiebreaker before paging, so Bediz promises no order among boards with equal creation times and does not re-sort a page. The result also carries the total count that InvokeAI reports.
- A board summary contains only `board_id`, `board_name`, `image_count`, `archived`, an optional `cover_image_name`, and the created and updated timestamps.
- `boards get SELECTOR` first tries an exact board identifier. If nothing matches, it searches every board, including archived ones, for an exact name that is case-sensitive. A single match wins. Several matches return `selection_required` with `kind: "board"`, and each candidate carries `board_id` and `board_name`. No match returns `not_found`.
- InvokeAI 6.14.1 shows an admin every board. It shows a non-admin their own boards, boards shared with them, and boards with `shared` or `public` visibility, including those owned by other users. Bediz reports that scope as is, and name lookup covers only visible boards.
- Read-only inspection may run on an untested but compatible InvokeAI version (§5).
- The expected 6.14.1 endpoints are the boards list and board detail routes under `/api/v1/boards/`. Confirm them against the live OpenAPI document before coding.

- [x] `boards list` works with flags and with a Request Document
- [x] `boards get` resolves an exact id and a unique name, and returns `selection_required` or `not_found` otherwise
- [x] Board output is allowlisted and normalized
- [x] `doctor` reports both operations
- [x] Spec §15 is updated

## Comments

### 2026-09-24 implementation

- Endpoints confirmed against the live 6.14.1 OpenAPI document: `GET /api/v1/boards/` (paged with `offset` and `limit`, unpaged array with `all=true`) and `GET /api/v1/boards/{board_id}`. `BoardDTO` carries every summary field.
- `boards get` Request Document: `{"schema_version":1,"board":"<id or name>"}`, mirroring the `model` selector. Result: `{"board": <summary>}`.
- The 6.14.1 detail route answers 404 for an absent board and 403 for another user's private board. Both fall through to name lookup, so an invisible board ends as `not_found`. The detail route checks only owner and visibility, not the `shared_boards` table, while the list includes it; no 6.14.1 API writes `shared_boards`, so the difference is not reachable and was not escalated.
- Live checks against InvokeAI 6.14.1 at `http://127.0.0.1:9090` with three throwaway boards (`bediz-live-check`, two `bediz-live-dup`, one archived), deleted afterward; the baseline has no boards again:
  - `boards list --json`: exit 0, newest first, archived board excluded, total 2.
  - `boards list --include-archived --limit 2 --json`: exit 0, archived board included.
  - `boards list --limit 0 --json`: exit 2, `invalid_request`.
  - `boards get <id> --json` and `boards get bediz-live-check --json`: exit 0, same summary.
  - `boards get bediz-live-dup --json`: exit 3, `selection_required`, `kind: "board"`, candidates sorted by id.
  - `boards get Bediz-Live-Check --json`: exit 6, `not_found` (case-sensitive).
  - `echo '{"schema_version":1,"board":"bediz-live-check"}' | boards get --request -`: exit 0.
  - `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e -run '^TestLiveGate$/^(doctor_verifies|model_listing|image_listing|queue_listing|board_listing)'`: PASS, doctor reports `boards.list` and `boards.get` compatible.
- Observation: InvokeAI rewrites `updated_at` without milliseconds after a PATCH (`2026-09-24 07:32:57`). Bediz passes timestamps through as reported, as it does for images.
