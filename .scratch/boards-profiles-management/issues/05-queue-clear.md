# 05: Queue clear

**What to build:** A human or agent can explicitly empty a queue. `bediz queue clear --yes` sends one clear request for the queue and reports how many items InvokeAI deleted.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned`. The operation is irreversible, it affects work the human may have queued, and its exact 6.14.1 effect on the in-progress item must be observed live before it is documented.

**Verification gate:** CLI-seam tests cover a missing `--yes` (`invalid_request`, exit 2, no network request), success, a conclusive rejection, and an inconclusive result (`outcome_unknown`). Tests prove the clear is sent exactly once and never retried. A live 6.14.1 observation records what happens to pending, in-progress, and completed items, and the spec states that observed effect. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Not required by this route.

**Escalate when:** The live effect differs from "cancels the running item and deletes every item in the queue", or the endpoint is not scoped to one queue.

**Permanent records:** V1 spec §15 records the observed destructive effect and the `--yes` requirement. Tests record the contract.

**Status:** ready-for-agent

## Accepted behavior

- The request has `queue_id` (default `default`). `--yes` is execution approval and is not a Request Document field. It is required even when the request comes from a document.
- The result data is `{"queue_id":..., "deleted": <count reported by InvokeAI>}`.
- Clearing requires a supported InvokeAI version.

- [ ] `--yes` is enforced before the network
- [ ] The clear is sent once, and an uncertain result returns `outcome_unknown`
- [ ] The live effect is recorded in spec §15
