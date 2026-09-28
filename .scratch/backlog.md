# Backlog after V1

Candidate work that V1 deliberately left out. An entry here is not a commitment: turn it into a feature directory with a spec and tickets when it is chosen. Remove the entry when its feature ships or is rejected.

## Creative operations

V1 spec section 2 lists these as outside V1.

- img2img
- Inpainting
- LoRAs
- Reference images
- ControlNet-guided generation
- Canvas brush and layer control
- Arbitrary workflow authoring
- Generation families beyond Anima, SDXL, and FLUX.1, and upscale families beyond SD1.5 and SDXL
- The SDNQ self-contained FLUX component fallback (spec §11.4)

## Management commands

- Board deletion, once its effect on contained images has a destructive-operation contract (spec §15)
- Batch or bulk queue cancellation (spec §15.1)
- Hugging Face variant, subfolder, and file references for user-supplied sources (spec §14.3)

## Platform and compatibility

- InvokeAI 6.15 and later, after live compatibility checks (spec §5)
- Socket.IO progress instead of polling (spec §4)
- Package-manager formulas, a self-updater, additional architectures, and signed installers (spec §21)

## Security hardening

From the V1 security review in `security-agent-review/spec.md`:

- Pin the `skills` package the install scripts run through `npx`, and add release provenance attestation (finding 6).
- Refuse a scheme downgrade on redirects of safe reads that carry a bearer token (finding 8).
