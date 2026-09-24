# 01: Classify gateway statuses on mutations

**What to decide and build:** Decide whether a 502, 503, or 504 answer to a mutation is a conclusive rejection or an inconclusive result, and apply the decision to every mutation the same way.

**Blocked by:** None.

**Status:** needs-triage

## Problem

Every mutation is sent once. A transport failure, an unreadable body, or an incomplete success body returns `outcome_unknown`. Any non-2xx status, including 502, 503, and 504, currently returns a conclusive failure (`invokeai_operation_failed`, or `authentication_failed` for 401/403). A reverse proxy between Bediz and InvokeAI can answer 502 or 504 after InvokeAI has already applied the mutation. An agent that reads the failure as conclusive may then submit the same mutation again.

Affected sends, as of `e978e5d`:

- `graphops.Enqueue` (`generate`, `upscale`): a repeat creates a duplicate batch. This is the highest risk.
- `images upload` and the upscale path-source upload: a repeat creates a duplicate image.
- `models install`: a repeat submits a second install job.
- `recall`, `auth huggingface login|logout`: repeating is harmless or idempotent.
- `boards create`: a repeat is caught by the duplicate-name check, which returns `board_name_exists` with the created board's identifier.

## Options

1. Keep the current behavior: every non-2xx status is conclusive.
2. Treat 502 and 504 on a mutation as `outcome_unknown`, and keep 503 conclusive, because 503 normally means the request was not processed.
3. Treat 502, 503, and 504 on a mutation as `outcome_unknown`.

Options 2 and 3 change public behavior of shipped operations and need a V1 spec §17 update and tests at the CLI seam for each affected operation.

## Comments

### 2026-09-24

- Raised by the independent review of `boards create` (ticket `boards-profiles-management/02`). Deferred from that slice because it changes shipped operations; `boards create` is already safe to repeat.
