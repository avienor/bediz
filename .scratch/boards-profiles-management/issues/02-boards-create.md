# 02: Boards create

**What to build:** An agent can create an explicit Output Board and get back its identifier for later `--board` use. `bediz boards create NAME`, or a Request Document `{"schema_version":1,"board_name":"..."}`, creates one board and returns its normalized board summary.

**Blocked by:** 01 (Boards list and get).

**Execution route:** `worker + independent review`. This is a single, reversible mutation (a board can be deleted in the UI), and the send-once contract can be tested directly.

**Verification gate:** CLI-seam tests cover an empty or whitespace-only name (exit 2 before the network), an existing exact name (`invalid_request`, `reason: "board_name_exists"`, and no mutation), including several existing boards with that name (`board_ids` lists all of them sorted by identifier), a successful create, a conclusive rejection, and an inconclusive transport result (`outcome_unknown`, exit 6). Tests prove that exactly one create request is sent and that it is never retried. `doctor` registers create only for the tested version range. A live create on 6.14.1 is observed in the InvokeAI gallery, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §15, §17, ADR 0010, and this ticket. Independently confirm that there is no retry on any failure path, that the duplicate check scans every board page including archived boards, and that the version gating is correct. A retry, a mutation sent after the duplicate-name check fails, or a missing `outcome_unknown` blocks acceptance.

**Escalate when:** The 6.14.1 create endpoint does not return the created board, or it takes the name somewhere that makes the send-once guarantee unclear.

**Permanent records:** V1 spec §15 gains the create contract and the duplicate-name rule. The rule does not change accepted terminology, so `CONTEXT.md` stays unchanged. Tests record the contract.

**Status:** done

## Accepted behavior

- The name is taken exactly as given, and a name that is empty after trimming is `invalid_request`.
- Before the mutation, Bediz lists all boards, including archived ones. If any has exactly the same name, it returns `invalid_request` with `reason: "board_name_exists"` and `board_ids`, every matching board identifier sorted ascending, and does not send the mutation. InvokeAI still allows duplicates created by other clients; `boards get` handles those through `selection_required`.
- The check covers every board visible to the calling user. InvokeAI 6.14.1 shows an admin every board. It shows a non-admin their own boards, boards shared with them, and boards with `shared` or `public` visibility, including those owned by other users. A visible board with the same name blocks creation whoever owns it. Only a board the caller cannot see does not block creation.
- The create request is sent once and never retried automatically. An inconclusive transport result, or a success response without a board identifier, returns `outcome_unknown`, and the caller inspects `boards list` before trying again.
- Creating a board requires a supported InvokeAI version.

- [x] Create works with a positional name and with a Request Document
- [x] A duplicate exact name is rejected before any mutation
- [x] The mutation is sent once, and an uncertain result returns `outcome_unknown`
- [x] Spec §15 is updated

## Comments

### 2026-09-24 implementation

- Endpoint confirmed against the live 6.14.1 OpenAPI document: `POST /api/v1/boards/` takes `board_name` as a required query parameter (max length 300) and answers 201 with the created `BoardDTO`. The name travels in the query string of a single request, so the send-once guarantee is clear and no escalation was needed.
- Order of work: local validation, then the supported-version check (`GET /api/v1/app/version`), then the duplicate-name check over `GET /api/v1/boards/?all=true&include_archived=true` (the unpaged every-visible-board mode `boards get` uses), then exactly one create request. The create goes through the non-retrying mutation path; a transport failure, an undecodable body, or a body without `board_id` returns `outcome_unknown`. Result: `{"board": <board summary>}`.
- Bediz does not check the 300-character limit locally; InvokeAI's 422 is a conclusive rejection (`invokeai_operation_failed`).
- Live checks against InvokeAI 6.14.1 at `http://127.0.0.1:9090` with throwaway boards, deleted afterward; the baseline has no boards again:
  - `boards create bediz-live-create --json`: exit 0, board summary with a new identifier.
  - The same command again: exit 2, `invalid_request`, `reason: "board_name_exists"`, `board_ids` holds the first board.
  - `echo '{"schema_version":1,"board_name":"Bediz-Live-Create"}' | boards create --request - --json`: exit 0 (case-sensitive match).
  - `boards create "   " --json`: exit 2, `invalid_request`.
  - `boards create bediz-live-human`: exit 0, human output `<id>\tbediz-live-human\t0`.
  - After archiving that board through the InvokeAI API, `boards create bediz-live-human --json`: exit 2, `board_name_exists` with the archived board's identifier.
  - `doctor --json`: ok, `boards.create` compatible with no failures.
  - InvokeAI web UI gallery at `http://127.0.0.1:9090`: `bediz-live-create` and `Bediz-Live-Create` appear in the board list; the archived board is hidden, which is the UI default.
