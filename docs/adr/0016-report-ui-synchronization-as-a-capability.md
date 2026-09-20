---
status: accepted
---

# Report UI synchronization as a capability

Bediz will treat UI synchronization as an explicit capability with `full` and `partial` levels. On stock InvokeAI 6.14.x, supported generation parameters can be synchronized fully through the Recall API. Generative upscale synchronization is partial because the Recall contract does not accept the upscale source image, Spandrel model, scale, creativity, structure, Tile ControlNet, tile size, or tile overlap.

Partial upscale synchronization means that the queue item, result image, embedded metadata, prompts and seed where supported, and Bediz execution receipt remain available for human inspection and handoff. It does not claim that the Upscale panel's operation-specific controls have been repopulated. `doctor` will report synchronization levels per operation.

Bediz will not use browser automation to bridge this gap. The project may contribute typed upscale fields to InvokeAI's Recall API and frontend. When a supported InvokeAI version exposes and consumes those fields, its capability entry may advertise full upscale synchronization.

## Consequences

Bediz remains compatible with standard InvokeAI installations without a browser extension or fork, while users and agents receive an accurate handoff guarantee. Full upscale-panel restoration depends on an upstream API capability rather than brittle manipulation of browser state.
