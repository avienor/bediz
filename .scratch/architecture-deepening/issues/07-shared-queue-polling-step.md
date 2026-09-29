# 07: Share one queue polling step between queue wait and graph operations

**What to build:** The queue module owns one step for polling a single queue item. The step covers:

- inspecting the item with `queue get`;
- the queue identity check;
- classifying the status against the tested status set: `pending`, `in_progress`, and `waiting` are non-terminal; `completed`, `failed`, and `canceled` are terminal; anything else is `invalid_invokeai_response`;
- backoff between polls;
- reporting why the wait stopped on timeout or interruption.

Two traversal policies use that step. Each keeps its current observable semantics:

- **`queue wait`** polls every pending item in rounds, returns failed and canceled items as data, and succeeds when all items are terminal. This is unchanged.
- **Graph operations (`generate`, `upscale`)** wait for their accepted items in queue item order and also check the batch identity. They return an operation failure (`invokeai_operation_failed`) at the first failed or canceled item. This is unchanged: with a failed first item and a later item that stays pending, generate still fails immediately rather than waiting or timing out.

Graph operations keep their completion checks: the seed check, final non-intermediate image selection, and, for upscale, scale verification. The duplicated status classification and the second stop-reporting implementation are deleted. A single round-based loop for both policies is rejected (see Modules 4 in the feature spec).

There is one public change. When `generate` or `upscale` returns `wait_timeout` or `interrupted`, its details now also include `pending_item_ids`, with the same meaning as in `queue wait`: the accepted items not yet observed as terminal when the wait stopped, in queue item order. For graph operations, that is the item being waited on plus every later accepted item.

Everything else stays the same:

- the other details: `queue_id`, `batch_id`, and `item_ids`;
- error codes, messages, and exit statuses;
- the order and number of queue inspection requests;
- every other wait failure, including an item missing during the wait and a contradictory queue or batch identity.

This is decision B of `.scratch/architecture-deepening/spec.md`. It is additive under V1 spec §25. Source of truth: V1 spec §11.5, §13.2, §15.1 (`queue wait`), and §17, ADR-0008, and ADR-0022.

**Blocked by:**
- 06 (Run upscale through the Direct Execution module), so that waiting is owned by the module for both operations;
- 02 (Classify Structured Errors outside the CLI adapter), so that the new details are covered by a table test.

**Execution route:** `worker + independent review`. The accepted behavior, including both traversal policies, is fully specified, the change is bounded, and public tests can detect a traversal change or a missing field.

**Verification gate:**
- New public CLI-seam tests:
  - `generate` with several outputs and `upscale` each return `pending_item_ids` on `wait_timeout` and on `interrupted`, and the value matches the definition above;
  - `generate` with a failed first item and a second item that stays pending returns `invokeai_operation_failed` for the first item, and does not time out.
- Existing `queue wait`, `generate`, and `upscale` tests pass with unmodified assertions. Existing timeout assertions check individual details members, so the added field does not break them.
- V1 spec §11.5 (and §13.2 where it refers to it) documents `pending_item_ids` for graph operations with the definition above.
- `CHANGELOG.md` records the change under `Unreleased`.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required, because no request InvokeAI receives changes and polling stays read-only. Record that in the ticket comments.

**Review gate:**
- A separate reviewer starts from this ticket, V1 spec §11.5 and §15.1, and a fixed base/head diff.
- The reviewer independently confirms each of the following:
  - one polling step remains, with one status classification and one stop-reporting implementation;
  - the tested status set, poll intervals, and request order are unchanged for both policies;
  - graph operations still check the batch identity and still fail at the first failed or canceled item;
  - `queue wait` still returns such items as data;
  - `pending_item_ids` matches the definition above.
- Blocking findings:
  - any changed code, message, or exit status;
  - any traversal change observable at the CLI seam other than the new field;
  - spec text that disagrees with the tests.

**Escalate when:**
- Sharing the step would change an existing wait failure's code, message, or details. Examples: an item missing during the wait, or a contradictory batch identity.
- The two traversal policies cannot share the step without changing request order.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec §11.5 and §13.2: the new details field and its definition for graph operations.
- `CHANGELOG.md` under `Unreleased`.
- Public tests.

**Status:** completed

- [x] `queue wait`, `generate`, and `upscale` share one queue polling step, and each keeps its traversal semantics.
- [x] Generate and upscale wait timeouts and interruptions report `pending_item_ids` as defined, and nothing else changes.
- [x] A test proves generate still fails immediately on a failed first item while a later item stays pending.
- [x] The V1 spec and `CHANGELOG.md` record the field.
- [x] All verification commands pass, and the absence of a live check is recorded.

## Comments

- 2026-09-29: The shared queue step preserves round-based `queue wait` and item-order graph waiting, including the graph wait's prior error messages and missing-item handling. Public CLI tests cover pending IDs, immediate failure, missing items, and batch identity. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed. No live InvokeAI check was required because polling remains read-only and no request sent to InvokeAI changed.
