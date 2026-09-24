# 03: Queue wait

**What to build:** After `generate --no-wait` or `upscale --no-wait`, a caller can block until the accepted items finish. `bediz queue wait ITEM_ID...` polls read-only queue inspection for one or more items until each reaches a terminal state. It then returns each item's normalized queue-item result in the order the items were requested.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The polling semantics are already specified in §11.5, the command reuses the existing tested waiting behavior, and it is read-only.

**Verification gate:** CLI-seam tests cover several ids (in order), duplicate ids (`invalid_request`), a completed item with image references, a failed or canceled item (returned as data, exit 0), an unknown status (`invalid_invokeai_response`), an absent item (`not_found`), `--timeout` (`wait_timeout` with the pending ids), and interruption (`interrupted`, exit 130). Tests prove that no cancel or enqueue request is ever sent. The request accepts both flags and a Request Document. A live wait on a `generate --no-wait` item on 6.14.1 is reported, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §11.5, §15, §17, and this ticket. Independently check the timeout and interrupt paths, the absence of mutations, and the result order. Silent termination on an unknown status, or any mutation, blocks acceptance.

**Escalate when:** Reusing the existing wait code would require changing the `generate` or `upscale` observable behavior.

**Permanent records:** V1 spec §15 records the wait result shape and states that terminal failure is data. Tests record the contract.

**Status:** ready-for-agent

## Accepted behavior

- The request has `queue_id` (default `default`) and `item_ids` (a non-empty list of non-negative integers without duplicates). The positional arguments compile to `item_ids`.
- The terminal statuses are `completed`, `failed`, and `canceled`, and the non-terminal statuses are `pending`, `in_progress`, and `waiting`. Any other status fails with `invalid_invokeai_response`.
- The result data is `{"queue_id":..., "items":[<queue get projection>...]}` in request order. A failed or canceled item still means the wait succeeded; the item's concise error is included when present.
- There is no total timeout by default. When `--timeout` elapses, the command returns `wait_timeout` with the item ids that are not yet terminal. Neither a timeout nor an interruption cancels anything remotely.

- [ ] Wait polls read-only until every item is terminal
- [ ] Timeout and interruption behave as specified
- [ ] `doctor` registers queue wait as read-only inspection
- [ ] Spec §15 is updated
