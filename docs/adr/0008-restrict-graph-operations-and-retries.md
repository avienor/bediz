---
status: accepted
---

# Restrict graph operations to tested versions and avoid unsafe retries

Graph-producing operations such as generation and upscale will run only when the InvokeAI version is within the operation's tested support range and its compatibility checks pass. On an untested minor version, Bediz will continue to offer diagnostics and compatible read-only inspection, but it will reject graph-producing operations with an `unsupported_capability` error. V1 will not provide an override for untested graph execution.

Bediz may retry safe read-only requests a limited number of times after transient connection failures. It will not automatically retry requests that enqueue, install, delete, cancel, or otherwise mutate state. When the connection fails after such a request may have reached InvokeAI, Bediz will return an `outcome_unknown` error and direct the caller to inspect the relevant queue, model, or image state before submitting anything again.

Ordinary HTTP and connection attempts will have bounded timeouts. Waiting for generation or a queue item will have no total timeout by default, with an optional caller-supplied `--timeout`. Interrupting Bediz stops only the local wait and does not cancel the InvokeAI operation.

## Consequences

Bediz favors verifiable behavior over optimistic compatibility with newly released InvokeAI versions. Users may need a Bediz compatibility update before graph operations work with a new InvokeAI minor release. Agents cannot turn a lost response into an accidental duplicate simply through an internal retry, and cancellation remains a separate explicit operation.
