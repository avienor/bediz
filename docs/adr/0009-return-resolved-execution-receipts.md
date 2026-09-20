---
status: accepted
---

# Return resolved execution receipts

A successful direct execution will return an execution receipt containing both the submitted request and the final settings after family defaults, profile values, component resolution, and explicit values have been applied. The receipt will identify the exact model and component keys, queue batch and item identifiers, every output seed, resulting image references, and any UI synchronization warnings. Parameter Recall will use these resolved settings rather than the unresolved input.

Bediz will resolve output seeds before graph submission. When no seed is supplied, each requested output will receive a valid random seed. When a seed is supplied for multiple outputs, the first output will use that seed and subsequent outputs will use sequentially incremented seeds. V1 will not expose additional seed-sequence modes.

## Consequences

Humans and agents can inspect exactly what Bediz executed even when the request relied on defaults or automatic component selection. A completed multi-output request is reproducible from the receipt. Bediz must respect each model family's valid seed range when generating or incrementing seeds and must associate returned images with their resolved seeds.
