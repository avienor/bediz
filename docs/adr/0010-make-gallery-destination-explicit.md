---
status: accepted
---

# Make the gallery destination explicit

Generation requests may identify an InvokeAI output board by its exact board identifier. When no board is supplied, Bediz will submit no board identifier and InvokeAI will display the resulting images as Uncategorized. Bediz will not infer the destination from the board currently selected in a browser.

V1 will provide `boards list`, `boards get`, and `boards create` so humans and agents can discover or establish an explicit destination. A board name may be accepted as a convenience only when it resolves uniquely. Board deletion is deferred because its effect on contained images requires a separate destructive-operation contract.

## Consequences

Generation requests remain independent of transient browser state and do not unexpectedly create a Bediz-specific board. Users who do not care about gallery organization can omit the field, while organized workflows can resolve and retain stable board identifiers.
