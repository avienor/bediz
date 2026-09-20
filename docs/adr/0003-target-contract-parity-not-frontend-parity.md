---
status: accepted
---

# Target contract parity instead of frontend implementation parity

The controller will guarantee equivalent generation behavior only for the operations, parameters, model families, and InvokeAI versions listed in its tested capability matrix. It will not attempt to reproduce every frontend branch, experimental feature, or hidden browser default. Unsupported combinations will fail explicitly rather than silently falling back, keeping the public contract verifiable while allowing InvokeAI's frontend to evolve independently.

## Consequences

Each supported capability requires versioned fixtures and tests before it is advertised. New InvokeAI frontend behavior does not automatically become controller behavior, and users may encounter explicit compatibility errors for unverified versions or options.
