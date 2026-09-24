# 04: Queue cancel

**What to build:** A human or agent can stop a queued or running item. `bediz queue cancel ITEM_ID` sends one cancellation for that item and returns its normalized queue item afterward. InvokeAI 6.14.1 cancels the non-terminal items in the named item's workflow-call chain, including the named item itself, and leaves terminal items unchanged.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. This is a single-item mutation with an explicit contract, and it is covered by send-once tests plus a live check.

**Verification gate:** CLI-seam tests cover a pending item, an in-progress item, an already terminal item (the result reports its unchanged terminal status, exit 0), a named item in a workflow-call chain with mixed statuses (the result reflects InvokeAI's response; the contract text does not claim that terminal chain items change), an absent item (`not_found`), a conclusive rejection, and an inconclusive transport result (`outcome_unknown`). Tests prove that the cancellation is sent exactly once and is never retried. No `--yes` is required. `doctor` registers cancel for the tested version range only. A live cancellation of a pending item on 6.14.1 is observed in the InvokeAI queue UI, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §15, §17, §18, and this ticket. Independently check the send-once guarantee and the already-terminal behavior against the real 6.14.1 response. A retry or a missing `outcome_unknown` blocks acceptance.

**Escalate when:** The 6.14.1 cancel endpoint cancels items outside the named item's workflow-call chain, or it returns an error for items that are already terminal.

**Permanent records:** V1 spec §15 records the per-item cancel contract, the workflow-call chain effect, and that batch cancellation is deferred. Tests record the contract.

**Status:** done

## Accepted behavior

- The request has `queue_id` (default `default`) and `item_id`. There is no batch or bulk selector in V1.
- The item id must be positive, matching `queue get`.
- InvokeAI 6.14.1 cancels the non-terminal items in the named item's workflow-call chain, and the named item itself counts as part of that chain. Completed, failed, and already canceled items keep their status. Bediz documents that effect and does not try to narrow it. Bediz's own generate and upscale batches create no chains, so for them at most the named item is canceled.
- The result data is the `queue get` projection of the named item after cancellation.
- The 6.14.1 endpoint is `PUT /api/v1/queue/{queue_id}/i/{item_id}/cancel`, confirmed against the installed source. Check it against the live OpenAPI document before coding.

- [x] Cancel works with a positional id and with a Request Document
- [x] The cancellation is sent once, and an uncertain result returns `outcome_unknown`
- [x] Spec §15 is updated

## Comments

### 2026-09-24 implementation

- The live 6.14.1 OpenAPI document lists `PUT /api/v1/queue/{queue_id}/i/{item_id}/cancel` with a `SessionQueueItem` 200 response. The installed source (`session_queue_sqlite.cancel_queue_item`) cancels only the named item's workflow-call chain and skips terminal items without an error, so no escalation was needed.
- `internal/queue` owns `queue.Cancel`. It checks the supported version range, sends one `PUT` through the non-retrying mutation path, and projects InvokeAI's cancel response with the same code as `queue get`. There is no follow-up read. A success response that names another item or queue, or no item, returns `outcome_unknown`.
- `doctor` registers `queue.cancel` for the supported range only (version, cancel route, and image detail for the projection).
- Live checks against InvokeAI 6.14.1 at `http://127.0.0.1:9090`:
  - `doctor --json`: `queue.cancel` compatible.
  - `queue cancel 87 --json` on a completed item: exit 0, status `completed` unchanged, with its Image Reference. `queue cancel 88 --json` on an already canceled item: exit 0, `canceled`.
  - `queue cancel 99999 --json`: exit 6, `not_found`.
  - Two one-output `generate --model "Anima Base 1.0" --width 768 --height 768 --no-wait` calls gave item 89 (`in_progress`) and item 90 (`pending`). `queue cancel 90 --json`: exit 0, `canceled`. `queue cancel 89 --json`: exit 0, `canceled`.
  - In the InvokeAI queue UI: item 90 was CANCELED with no GPU or time, item 89 was CANCELED after 4.52 s, and item 87 was still COMPLETED. The counters showed 0 in progress and 0 pending. Neither canceled item produced a gallery image.

### 2026-09-24 review

- The standards and spec reviews found no blocker. The spec review checked the send-once path in the HTTP client and the already-terminal behavior in the installed 6.14.1 source.
- Applied: the command's short help no longer says the whole chain is canceled, and the shared queue-item projection now returns an `Item` directly.
- Open, not changed:
  - After a successful cancel, a failed Image Reference read for an item with image outputs reports a read failure, although the cancellation was applied. The item is usually already completed in that case.
  - A 403 for another user's item maps to `authentication_failed`, as it does for other commands.
  - The 503 gateway case follows `.scratch/mutation-gateway-status`.
  - "workflow-call chain" has no `CONTEXT.md` entry.
  - The get and cancel argument parsing is duplicated.
