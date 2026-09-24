---
status: accepted
---

# Treat gateway statuses on mutations as unknown outcomes

This extends ADR-0008. A 502, 503, or 504 answer to a mutation returns `outcome_unknown`, the same as a lost connection. The structured details carry the `status`. Bediz still sends the mutation once. Other non-2xx answers remain conclusive rejections.

InvokeAI 6.14.1 answers none of the routes Bediz mutates with these statuses; its only 503 belongs to an image-move route Bediz does not use. A gateway status therefore comes from an intermediary such as a reverse proxy. Bediz cannot tell whether that intermediary forwarded the request before it failed. Some proxies answer 503 as well as 502 when the upstream resets after receiving a request, so 503 gets the same treatment as 502 and 504.

The considered alternatives were keeping every non-2xx answer conclusive, and treating only 502 and 504 as unknown. Either would let an agent read a failure as conclusive after the mutation may already have been applied, and then repeat it. For enqueue, upload, install, and queue clear, a repeat creates duplicate work or deletes items enqueued in between.

## Consequences

A caller inspects remote state after a gateway status instead of resubmitting. For idempotent mutations such as Recall, Hugging Face login, and queue cancel, that inspection is the only extra cost. A generation or upscale whose post-enqueue Recall gets a gateway status still succeeds with the existing `ui_sync_failed` warning. Safe reads keep retrying gateway statuses within the configured limit.
