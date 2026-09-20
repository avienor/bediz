---
status: accepted
---

# Use an API-first companion controller

The controller will remain independent from InvokeAI and translate self-contained operations into version-aware execution graphs submitted through InvokeAI's API. It will synchronize the latest operation back to the web interface through the Recall API. Browser automation is not the primary control path because it couples the product to transient UI structure and requires a live browser; an InvokeAI fork is avoided so the controller can work with standard installations.

## Consequences

The controller must maintain and test graph adapters for each supported InvokeAI version and model family. Browser automation may be considered only as a narrow fallback for a valuable operation that has no viable API path.
