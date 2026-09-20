---
status: accepted
---

# Implement the controller in Go

The controller will be implemented as a standalone Go application and distributed as a single binary. It will communicate with standard InvokeAI installations through HTTP APIs, produce structured JSON output, compile normalized generation requests with versioned model-family adapters, and initially use polling for operation status. This avoids coupling distribution to InvokeAI's Python environment or its private, browser-coupled TypeScript frontend runtime.

## Consequences

The project must implement and maintain its own basic execution-graph builders for supported model families. InvokeAI's live OpenAPI schema will be used for capability and payload validation, but it cannot supply graph topology or model-family composition rules.
