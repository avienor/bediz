# 02: Boards create

**What to build:** An agent can create an explicit Output Board and get back its identifier for later `--board` use. `bediz boards create NAME`, or a Request Document `{"schema_version":1,"board_name":"..."}`, creates one board and returns its normalized board summary.

**Blocked by:** 01 (Boards list and get).

**Execution route:** `worker + independent review`. This is a single, reversible mutation (a board can be deleted in the UI), and the send-once contract can be tested directly.

**Verification gate:** CLI-seam tests cover an empty or whitespace-only name (exit 2 before the network), an existing exact name (`invalid_request`, `reason: "board_name_exists"`, and no mutation), including several existing boards with that name (`board_ids` lists all of them sorted by identifier), a successful create, a conclusive rejection, and an inconclusive transport result (`outcome_unknown`, exit 6). Tests prove that exactly one create request is sent and that it is never retried. `doctor` registers create only for the tested version range. A live create on 6.14.1 is observed in the InvokeAI gallery, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §15, §17, ADR 0010, and this ticket. Independently confirm that there is no retry on any failure path, that the duplicate check scans every board page including archived boards, and that the version gating is correct. A retry, a mutation sent after the duplicate-name check fails, or a missing `outcome_unknown` blocks acceptance.

**Escalate when:** The 6.14.1 create endpoint does not return the created board, or it takes the name somewhere that makes the send-once guarantee unclear.

**Permanent records:** V1 spec §15 gains the create contract and the duplicate-name rule. The rule does not change accepted terminology, so `CONTEXT.md` stays unchanged. Tests record the contract.

**Status:** ready-for-agent

## Accepted behavior

- The name is taken exactly as given, and a name that is empty after trimming is `invalid_request`.
- Before the mutation, Bediz lists all boards, including archived ones. If any has exactly the same name, it returns `invalid_request` with `reason: "board_name_exists"` and `board_ids`, every matching board identifier sorted ascending, and does not send the mutation. InvokeAI still allows duplicates created by other clients; `boards get` handles those through `selection_required`.
- The check covers only boards visible to the calling user, which is InvokeAI 6.14.1's per-user scope for non-admin callers. A same-named board owned by another user does not block creation.
- The create request is sent once and never retried automatically. An inconclusive transport result, or a success response without a board identifier, returns `outcome_unknown`, and the caller inspects `boards list` before trying again.
- Creating a board requires a supported InvokeAI version.

- [ ] Create works with a positional name and with a Request Document
- [ ] A duplicate exact name is rejected before any mutation
- [ ] The mutation is sent once, and an uncertain result returns `outcome_unknown`
- [ ] Spec §15 is updated
