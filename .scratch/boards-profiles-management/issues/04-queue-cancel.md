# 04: Queue cancel

**What to build:** A human or agent can stop a queued or running item. `bediz queue cancel ITEM_ID` sends one cancellation for that item and returns its normalized queue item afterward. InvokeAI 6.14.1 cancels the non-terminal items in the named item's workflow-call chain, including the named item itself, and leaves terminal items unchanged.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. This is a single-item mutation with an explicit contract, and it is covered by send-once tests plus a live check.

**Verification gate:** CLI-seam tests cover a pending item, an in-progress item, an already terminal item (the result reports its unchanged terminal status, exit 0), a named item in a workflow-call chain with mixed statuses (the result reflects InvokeAI's response; the contract text does not claim that terminal chain items change), an absent item (`not_found`), a conclusive rejection, and an inconclusive transport result (`outcome_unknown`). Tests prove that the cancellation is sent exactly once and is never retried. No `--yes` is required. `doctor` registers cancel for the tested version range only. A live cancellation of a pending item on 6.14.1 is observed in the InvokeAI queue UI, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §15, §17, §18, and this ticket. Independently check the send-once guarantee and the already-terminal behavior against the real 6.14.1 response. A retry or a missing `outcome_unknown` blocks acceptance.

**Escalate when:** The 6.14.1 cancel endpoint cancels items outside the named item's workflow-call chain, or it returns an error for items that are already terminal.

**Permanent records:** V1 spec §15 records the per-item cancel contract, the workflow-call chain effect, and that batch cancellation is deferred. Tests record the contract.

**Status:** ready-for-agent

## Accepted behavior

- The request has `queue_id` (default `default`) and `item_id`. There is no batch or bulk selector in V1.
- The item id must be positive, matching `queue get`.
- InvokeAI 6.14.1 cancels the non-terminal items in the named item's workflow-call chain, and the named item itself counts as part of that chain. Completed, failed, and already canceled items keep their status. Bediz documents that effect and does not try to narrow it. Bediz's own generate and upscale batches create no chains, so for them at most the named item is canceled.
- The result data is the `queue get` projection of the named item after cancellation.
- The 6.14.1 endpoint is `PUT /api/v1/queue/{queue_id}/i/{item_id}/cancel`, confirmed against the installed source. Check it against the live OpenAPI document before coding.

- [ ] Cancel works with a positional id and with a Request Document
- [ ] The cancellation is sent once, and an uncertain result returns `outcome_unknown`
- [ ] Spec §15 is updated
