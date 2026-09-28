---
status: accepted
---

# Keep schema version 1 additive after V1

After v1.0.0, work continues through individual tickets instead of the planned V1 slices. We keep `docs/spec/v1.md` as the living contract for schema version 1 and allow only additive changes under it; any change that could break a caller relying on the documented JSON, flags, defaults, error codes, exit statuses, or mutation rules requires schema version 2 and a new specification. Result consumers must tolerate added fields, while Request Documents stay strict.

## Considered Options

Starting a new specification file for every release was rejected because it would scatter one contract across files. Freezing `v1.md` and recording later features only in tickets was rejected because the specification would stop describing the shipped behavior.

## Consequences

Bediz release versions follow semantic versioning independently from the schema version. `CHANGELOG.md` records what each release changes.
