---
status: accepted
---

# Use a single versioned JSON result envelope

When machine-output mode is requested, Bediz will write exactly one final JSON result envelope to standard output. The envelope will include `schema_version`, `ok`, `operation`, `data` or a structured `error`, and `warnings`. Progress and diagnostic output will not be mixed into standard output; any necessary operational messages belong on standard error.

Request documents and result envelopes will initially require `schema_version: 1`. Request validation will reject unknown fields rather than silently ignoring them. Bediz will not provide a JSON Lines progress stream in V1. Callers that do not want to wait will submit with `--no-wait` and inspect or wait for the resulting queue item separately.

Structured error codes will carry specific failure meaning. Process exit statuses will remain a small stable set:

- `0` for success
- `2` for an invalid request
- `3` when a selection is required
- `4` for an incompatible or unsupported capability
- `5` for connection or authentication failure
- `6` for an InvokeAI operation failure
- `130` when interrupted by the user

## Consequences

Agents can parse standard output without filtering logs or progress records, and scripts can branch coarsely on exit status or precisely on the structured error code. Long-running commands produce no incremental machine-readable events in V1. Schema evolution requires an explicit version change or a backward-compatible interpretation under the current version.
