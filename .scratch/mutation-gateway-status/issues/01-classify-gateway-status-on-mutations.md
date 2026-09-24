# 01: Classify gateway statuses on mutations

**What to decide and build:** Decide whether a 502, 503, or 504 answer to a mutation is a conclusive rejection or an inconclusive result, and apply the decision to every mutation the same way.

**Blocked by:** None.

**Status:** done

## Problem

Every mutation is sent once. A transport failure, an unreadable body, or an incomplete success body returns `outcome_unknown`. Any non-2xx status, including 502, 503, and 504, currently returns a conclusive failure (`invokeai_operation_failed`, or `authentication_failed` for 401/403). A reverse proxy between Bediz and InvokeAI can answer 502 or 504 after InvokeAI has already applied the mutation. An agent that reads the failure as conclusive may then submit the same mutation again.

Affected sends, as of `e978e5d`:

- `graphops.Enqueue` (`generate`, `upscale`): a repeat creates a duplicate batch. This is the highest risk.
- `images upload` and the upscale path-source upload: a repeat creates a duplicate image.
- `models install`: a repeat submits a second install job.
- `recall`, `auth huggingface login|logout`: repeating is harmless or idempotent.
- `boards create`: a repeat is caught by the duplicate-name check, which returns `board_name_exists` with the created board's identifier.
- `queue cancel` (added after `e978e5d`): repeating is harmless, because canceling an already terminal item succeeds with its unchanged status.
- `queue clear` (added after `e978e5d`): a repeat clears again, which also deletes any items enqueued since the first clear. The repeat's `deleted` count covers only those later items.

## Options

1. Keep the current behavior: every non-2xx status is conclusive.
2. Treat 502 and 504 on a mutation as `outcome_unknown`, and keep 503 conclusive, because 503 normally means the request was not processed.
3. Treat 502, 503, and 504 on a mutation as `outcome_unknown`.

Options 2 and 3 change public behavior of shipped operations and need a V1 spec §17 update and tests at the CLI seam for each affected operation.

## Comments

### 2026-09-24

- Raised by the independent review of `boards create` (ticket `boards-profiles-management/02`). Deferred from that slice because it changes shipped operations; `boards create` is already safe to repeat.

### 2026-09-24 decision

- Decided: option 3, recorded in ADR-0020 and V1 spec §17. InvokeAI 6.14.1 answers none of the routes Bediz mutates with 502, 503, or 504. Its only 503 is on the image-move route, which Bediz does not use. So these statuses always come from an intermediary, and some proxies answer 503 after an upstream reset.
- `httpclient` returns `OutcomeUnknownError` with the status for a gateway status on `DoJSON` and `PostStream` mutations. `DoJSONPrivate` keeps the status and nothing else. The CLI adds `details.status`.
- CLI-seam tests cover 502, 503, and 504 for `generate`, `upscale`, `images upload`, `recall`, `models install`, and `auth huggingface login|logout`. The `boards create`, `queue cancel`, and `queue clear` failure tables have a 503 row. The Recall fixtures in the generate and upscale sync tests now answer 500, so the conclusive rejection path keeps its coverage.
