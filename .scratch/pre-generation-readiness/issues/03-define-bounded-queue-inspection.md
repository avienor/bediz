# 03: Define bounded queue inspection

**What to build:** Align the queue inspection safety contract with the supported backend API. A queue list request returns and hydrates no more than its requested page of lightweight summaries and never fetches execution graphs. Because the baseline item-ID endpoint has no pagination, Bediz may retrieve the complete lightweight ID index to preserve accurate ordering and total count, but that response remains subject to the configured HTTP response-size limit. The V1 specification and tests must describe this precise boundary instead of implying that all network work is proportional to the requested page limit.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned` — this reconciles an accepted safety contract with a verified backend limitation and therefore needs ownership of the public wording and trade-off.

**Verification gate:** A large item-ID fixture proves that only the requested page is sent for summary hydration, no execution graph is fetched, ordering and total count remain correct, and an oversized ID response fails at the HTTP response-size boundary. The accepted V1 specification matches the observable behavior. The supported live OpenAPI contract is checked for the absence of ID pagination, and all project-defined Go verification commands pass.

**Escalate when:** A supported backend exposes a reliable paginated ID endpoint; preserving an accurate total count would require unbounded memory or removing the response-size guard; live behavior differs from the OpenAPI contract; or the product should instead sacrifice total-count accuracy to stop reading the ID response early.

**Status:** ready-for-agent

- [ ] Queue output and summary hydration are bounded by the requested page and exclude execution graphs.
- [ ] Complete lightweight ID-index retrieval is constrained by the HTTP response-size limit and documented explicitly.
- [ ] Stress-oriented contract tests distinguish page-bounded work from backend ID-index retrieval.
- [ ] Live contract evidence and all project-defined Go verification commands pass.
