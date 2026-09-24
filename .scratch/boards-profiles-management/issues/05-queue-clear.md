# 05: Queue clear

**What to build:** A human or agent can explicitly clear a queue within their InvokeAI authorization scope. `bediz queue clear --yes` sends one clear request for the queue and reports how many items InvokeAI deleted.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned`. The operation is irreversible, it affects work the human may have queued, and its exact 6.14.1 effect on the in-progress item must be observed live before it is documented.

**Verification gate:** CLI-seam tests cover a missing `--yes` (`invalid_request`, exit 2, no network request), success, a conclusive rejection, and an inconclusive result (`outcome_unknown`). Tests prove the clear is sent exactly once and never retried. A live 6.14.1 observation records what happens to pending, in-progress, and completed items, and the spec states that observed effect. Run the live check only on an isolated InvokeAI 6.14.1 instance started for this check, with its own root directory and port, and with no other client connected. Never run it on the shared baseline at `127.0.0.1:9090`. An empty-queue check there does not isolate the test, because another client can enqueue between the check and the clear, and an admin clear deletes that item too. Report the caller's scope (single-user default admin, or multi-user). If no isolated instance can be started, report live verification as unavailable instead of falling back to the baseline. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Not required by this route.

**Escalate when:** The live effect differs from the scope described below, the endpoint is not limited to one queue, or an isolated instance cannot be started.

**Permanent records:** V1 spec §15 records the observed destructive effect and the `--yes` requirement. Tests record the contract.

**Status:** done

## Accepted behavior

- The request has `queue_id` (default `default`). `--yes` is execution approval and is not a Request Document field. It is required even when the request comes from a document.
- Scope follows InvokeAI 6.14.1: an admin caller (including the single-user default) cancels and deletes every item in the queue, and any other caller cancels and deletes only their own items. Bediz neither widens nor narrows that scope, and the spec and human-readable help say so.
- The endpoint is `PUT /api/v1/queue/{queue_id}/clear`.
- The result data is `{"queue_id":..., "deleted": <count reported by InvokeAI>}`.
- Clearing requires a supported InvokeAI version.

- [x] `--yes` is enforced before the network
- [x] The clear is sent once, and an uncertain result returns `outcome_unknown`
- [x] The live effect is recorded in spec §15

## Comments

### 2026-09-24 implementation

- The installed 6.14.1 source (`session_queue.clear` router and `session_queue_sqlite.clear`) passes `user_id=None` for an admin caller and the caller's own id otherwise, cancels every in-progress item in that scope, then deletes every row in `queue_id` in that scope, and returns `ClearResult{deleted}`. That matches the accepted scope, so no escalation was needed.
- `internal/queue` owns `queue.Clear`. It validates the schema version, queue id, and approval, checks the supported version range, and sends one `PUT` through the non-retrying mutation path. A success without a non-negative `deleted` count returns `outcome_unknown`. `--yes` sets `ClearRequest.Approved`, which has no JSON tag, so a document `yes` field is rejected as unknown.
- `doctor` registers `queue.clear` for the supported range only (version and clear route).
- Live check on an isolated InvokeAI 6.14.1 instance (`invokeai-web` from the baseline venv, its own fresh root in a temporary directory, `127.0.0.1:9191`, `INVOKEAI_DEVICE=cpu`, no other client connected), stopped afterwards. The shared baseline at `127.0.0.1:9090` was not touched. Caller scope: single-user mode (`multiuser_enabled: false`), which makes the caller the default admin. Queue items used a model-free `range_of_size` → `iterate` → `add` graph, 300 iterations for a quick item and 30000 for a long-running one.
  - Before the clear: item 1 `completed`, item 2 `in_progress`, item 3 `pending`.
  - `queue clear --json` without `--yes`: exit 2, `invalid_request`, and the queue status was unchanged.
  - `queue clear --yes --json`: exit 0, `{"queue_id":"default","deleted":3}`. Afterwards the queue status reported total 0 and `is_processing: false`, and `GET /api/v1/queue/default/i/{1,2,3}` each returned 404. The in-progress item stopped executing and was deleted along with the pending and completed items.
  - With one completed item in queue `other` and one in `default`, `queue clear --yes --queue-id default --json` reported `deleted: 1`, and queue `other` still held its completed item. The endpoint is limited to one queue.
  - Multi-user (non-admin) scope was not exercised live. The source restricts it to the caller's own items, and the spec records that.
