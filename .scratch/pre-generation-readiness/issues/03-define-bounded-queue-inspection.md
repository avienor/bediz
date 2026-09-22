# 03: Define bounded queue inspection

**What to build:** Align the queue inspection safety contract with the supported backend API. A queue list request returns and hydrates no more than its requested page of lightweight summaries and never fetches execution graphs. Because the baseline item-ID endpoint has no pagination, Bediz may retrieve the complete lightweight ID index to preserve accurate ordering and total count, but that response remains subject to the configured HTTP response-size limit. The V1 specification and tests must describe this precise boundary instead of implying that all network work is proportional to the requested page limit.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned` — this reconciles an accepted safety contract with a verified backend limitation and therefore needs ownership of the public wording and trade-off.

**Verification gate:** A large item-ID fixture proves that only the requested page is sent for summary hydration, no execution graph is fetched, ordering and total count remain correct, and an oversized ID response fails at the HTTP response-size boundary. The accepted V1 specification matches the observable behavior. The supported live OpenAPI contract is checked for the absence of ID pagination, and all project-defined Go verification commands pass.

**Escalate when:** A supported backend exposes a reliable paginated ID endpoint; preserving an accurate total count would require unbounded memory or removing the response-size guard; live behavior differs from the OpenAPI contract; or the product should instead sacrifice total-count accuracy to stop reading the ID response early.

**Status:** done

- [x] Queue output and summary hydration are bounded by the requested page and exclude execution graphs.
- [x] Complete lightweight ID-index retrieval is constrained by the HTTP response-size limit and documented explicitly.
- [x] Stress-oriented contract tests distinguish page-bounded work from backend ID-index retrieval.
- [x] Live contract evidence and all project-defined Go verification commands pass.

## Comments

Implemented the bounded queue-inspection contract. The production implementation already selected the requested ID page before summary hydration, restored the backend ID-index order after hydration, omitted execution graphs, and used the shared HTTP client's response-size guard. This slice therefore adds missing contract evidence and corrects the public wording rather than changing runtime behavior.

**Contract tests.** A 10,000-item queue fixture requests a seven-item page from the middle of the ordered ID index. It proves that exactly those seven IDs are sent once to the lightweight summary endpoint, only seven summaries are returned in index order even when hydration responds in reverse order, the total remains 10,000, and no per-item execution-graph endpoint is requested. A separate fixture configures a 128-byte HTTP response maximum and proves that an oversized complete ID index fails as an invalid InvokeAI response before summary hydration begins.

**Specification.** V1 inspection safety and the README now distinguish page-bounded public output and summary hydration from complete lightweight ID-index retrieval. They state that the supported 6.14.x item-ID endpoint has no pagination, the full index preserves ordering and reported total count, and its response remains bounded by the configured HTTP response-size limit.

**Live contract evidence.** The local supported baseline reported InvokeAI `6.14.1`. Its live OpenAPI document exposes only `queue_id` and `order_dir` on `GET /api/v1/queue/{queue_id}/item_ids`; it exposes no offset, limit, cursor, page, or other ID-pagination parameter. The live response schema requires the ordered `item_ids` collection and `total_count`.

**Verification evidence.** `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` all pass. Focused queue, HTTP-client, and public CLI test suites also pass.
