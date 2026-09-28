---
status: accepted
---

# Express image-to-image as a Source Image on the Generation Request

Image-to-image extends `generate` with a Source Image and optional Denoising Strength. The Generation Mode is inferred from the presence of `source`: without it, text-to-image; with it, image-to-image. The existing request, graph submission, waiting, and Execution Receipt interfaces continue to apply.

When a source is present, explicit dimensions take precedence. Otherwise its dimensions are rounded down to the supported family alignment. Profile dimensions are excluded as defaults, while profile applicability validation still runs. Denoising Strength uses InvokeAI's stock web-interface meaning; its default is 0.75.

`doctor` has a separate capability entry for each tested generation mode. The SDXL image-to-image entry requires the source inspection and upload endpoints and the encoder, resize, denoiser, and metadata vocabulary used by the graph. Other family modes enter the matrix only after their graphs are tested.

## Considered alternative

A separate image-to-image command would duplicate Generation Request settings, component resolution, queue handling, and receipts, so it was rejected.

## Consequences

The source is the only mode selector. A local path is validated before network access and uploaded once after all preflight checks. The Execution Receipt identifies the source and whether Bediz uploaded it. Browser Canvas state remains outside the controller.
