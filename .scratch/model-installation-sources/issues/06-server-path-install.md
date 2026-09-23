# 06: Register a server-local path and explicitly move files

**What to build:** `models install` accepts `type: path` and an exact reference interpreted in InvokeAI's server filesystem namespace. The default registers the model in place. A request to move it into InvokeAI-managed storage requires both the operation's `move` setting and execution approval `--yes`; without either, no moving mutation occurs. Flags and Request Documents compile to the same typed operation, while `--yes` may accompany a document as execution control. The returned installation job remains inspectable through `models status`.

**Blocked by:** 01: Install a model from an exact URL and inspect its job.

**Execution route:** `frontier-owned` — moving server files can be difficult to reverse, and the exact effect must be established on the target installation.

**Verification gate:** Public-interface and transport tests establish server-side path interpretation, in-place default, rejection of `move` without `--yes`, flag/document equivalence, single submission, job inspection, and `outcome_unknown` for an inconclusive move. A live check uses only an isolated test-owned path on local InvokeAI 6.14.1 and inspects both job and resulting file location; if a suitable fixture is unavailable, report that limitation. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`.

**Review gate:** Not required by this route.

**Escalate when:** In-place registration changes or deletes the source, move semantics contradict the accepted V1 spec, path interpretation differs between client and server, the test would touch pre-existing files, or repeated repairs fail.

**Permanent records:** V1 spec and tests — specify `move` request semantics, `--yes` approval, and observed in-place versus managed-storage behavior. An ADR is needed only if the accepted storage policy changes.

**Status:** done

- [x] A server-local path installs in place by default and leaves the source intact.
- [x] Moving requires explicit `move` and `--yes`, and only affects the requested path.
- [x] Job status and uncertain outcomes remain observable without automatic retry.

## Comments

Implemented in `da633c3`. Verification passed: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. On local InvokeAI 6.14.1, isolated test-owned copies completed as inspectable jobs: in-place registration retained its source file, and approved move removed its source copy and placed the model under InvokeAI-managed storage. Both test registrations were removed afterward. Fixed-diff standards and spec review found no remaining issues.
