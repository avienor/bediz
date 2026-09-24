# 03: Queue wait

**What to build:** After `generate --no-wait` or `upscale --no-wait`, a caller can block until the accepted items finish. `bediz queue wait ITEM_ID...` polls read-only queue inspection for one or more items until each reaches a terminal state. It then returns each item's normalized queue-item result in the order the items were requested.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The polling semantics are already specified in §11.5 and the command is read-only. Its observation loop is new, but tests can verify it fully.

**Verification gate:** CLI-seam tests cover several ids (in order), duplicate ids (`invalid_request`), a zero or negative id (`invalid_request`, before the network), a completed item with image references, a failed or canceled item (returned as data, exit 0), an unknown status (`invalid_invokeai_response`), an absent item (`not_found`), `--timeout` (`wait_timeout` with the pending ids), and interruption (`interrupted`, exit 130). Tests prove that no cancel or enqueue request is ever sent. The request accepts both flags and a Request Document. A live wait on a `generate --no-wait` item on 6.14.1 is reported, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §11.5, §15, §17, and this ticket. Independently check the timeout and interrupt paths, the absence of mutations, and the result order. Silent termination on an unknown status, or any mutation, blocks acceptance.

**Escalate when:** Sharing the polling interval or the status set with the existing wait code would change the observable behavior of `generate` or `upscale`.

**Permanent records:** V1 spec §15 records the wait result shape and states that terminal failure is data. Tests record the contract.

**Status:** done

## Accepted behavior

- The request has `queue_id` (default `default`) and `item_ids` (a non-empty list of positive integers without duplicates, matching `queue get`). The positional arguments compile to `item_ids`.
- The terminal statuses are `completed`, `failed`, and `canceled`, and the non-terminal statuses are `pending`, `in_progress`, and `waiting`. Any other status fails with `invalid_invokeai_response`.
- The result data is `{"queue_id":..., "items":[<queue get projection>...]}` in request order. A failed or canceled item still means the wait succeeded; the item's concise error is included when present.
- The existing generate and upscale wait cannot be reused as is: it turns failed or canceled items into errors, and it requires a batch identity and seeds. `queue wait` needs its own observation loop. That loop may share the polling intervals and the tested status set, and it must not check a batch identity.
- There is no total timeout by default. When `--timeout` elapses, the command returns `wait_timeout` with the item ids that are not yet terminal. Neither a timeout nor an interruption cancels anything remotely.

- [x] Wait polls read-only until every item is terminal
- [x] Timeout and interruption behave as specified
- [x] `doctor` registers queue wait as read-only inspection
- [x] Spec §15 is updated

## Comments

### 2026-09-24 implementation

- `internal/queue` owns the observation loop (`queue.Wait`) and now exports the tested status set and poll intervals; `graphops` uses the same constants, so generate and upscale waiting is unchanged. No escalation was needed.
- Each round polls, in request order, only the items not yet observed as terminal, so a timeout or interruption reports exactly the pending items. No batch identity is checked; a response that reports another item or queue identity is `invalid_invokeai_response`.
- Failure details: `wait_timeout` and `interrupted` carry `queue_id`, the requested `item_ids`, and `pending_item_ids` in request order; `batch_id` is omitted because queue wait has none. An untested status adds `item_id` and `status`. An absent item is `not_found` naming the id.
- Transient read failures use the HTTP client's bounded read retries, as generate waiting does.
- `--timeout` bounds only local waiting and may accompany `--request`; each HTTP request keeps its default transport timeout. Human output prints one line per item and appends a failed item's concise error.
- Live checks against InvokeAI 6.14.1 at `http://127.0.0.1:9090`:
  - `doctor --json`: ok, `queue.wait` compatible.
  - `generate --model "Anima Base 1.0" --prompt "a lighthouse in a storm" --width 768 --height 768 --output-count 2 --no-wait --json`: items `[87, 86]`.
  - `queue wait 87 86 --timeout 1s --json`: exit 6, `wait_timeout`, `pending_item_ids` `[87, 86]`.
  - `queue wait 87 86 --json` sent SIGINT after 2 s: `interrupted`, pending `[87, 86]`; both items later completed, so nothing was canceled.
  - `queue wait 87 86 --json`: exit 0 after about 77 s, both `completed` with one Image Reference each, in request order.
  - A one-output `generate --no-wait` gave item 88; `queue wait 88 --json` with SIGINT: exit 130. After canceling item 88 through the InvokeAI API, `queue wait 88 --json`: exit 0, status `canceled` returned as data.
  - `queue wait 999999 --json`: exit 6, `not_found`. `queue wait 87 87 --json`: exit 2, `invalid_request`. `queue wait 88 87`: human output in request order.
  - Gallery images created: `e189e208-8e64-421d-a3ed-0ded67f41567.png`, `920a461d-e45c-4373-9151-4f645d5e8cb9.png`.

### 2026-09-24 review

- Independent standards and spec reviews found no blocker. Applied: a failed item's concise error in human output, a clearer slice allocation, and a test for an interruption during an in-flight read.
- Not changed: the generate and queue wait loops still share only constants, because the generate loop turns failures into errors and checks batch identity; merging them would change more than this slice.
