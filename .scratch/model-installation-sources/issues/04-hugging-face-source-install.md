# 04: Install a model from a Hugging Face reference

**What to build:** `models install` accepts a plain Hugging Face `org/repo` identifier or canonical HTTPS repository URL through matching flags or a schema-version-1 Request Document with `source.type: huggingface` and `source.reference`. Normalize a plain ID to the canonical HTTPS repository URL before submission to avoid the tested backend's raw-ID server-path collision. Reject variant and subfolder reference forms until they are tested. Public repositories work without login. A protected repository requires both a valid InvokeAI Hugging Face login for metadata access and `--token-stdin` for the file download; Bediz never reads back or stores the login token. Reject missing credentials before installation. Submit through the tested generic POST installation endpoint that returns a `ModelInstallJob`; do not call the mutating Hugging Face GET endpoint, whose response does not provide an inspectable job ID and whose method could trigger safe-read retries. The result contains a job ID that `models status` can inspect. Bediz does not discover repositories, choose among ambiguous artifacts, store an authentication token, or invent component dependencies.

**Blocked by:** 01: Install a model from an exact URL and inspect its job; 02: Install a protected URL with a temporary token; 03: Manage Hugging Face authentication through InvokeAI.

**Execution route:** `frontier-owned` — the tested backend has source-typing and split metadata/download authentication behavior that require careful live judgment.

**Verification gate:** Public-interface tests prove flag/document equivalence, explicit source typing, canonicalization of a plain repo ID, rejection of variant/subfolder forms, a raw-ID/local-path collision fixture, public install, protected install requiring both valid login and `--token-stdin`, inspectable job IDs, and structured authentication failures before mutation. Transport tests assert generic POST rather than the mutating GET endpoint, exactly one submission, no token leakage, and `outcome_unknown` after inconclusive submission. `doctor` checks the exact POST method and response capability before advertising support. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`; exercise a small public reference against local InvokeAI 6.14.1 and a protected reference only with an authorized disposable fixture. If private live verification is unavailable, record that limit and do not claim private-source compatibility from endpoint presence alone.

**Review gate:** Not required by this route.

**Escalate when:** InvokeAI requires a credential path inconsistent with the accepted authentication boundary, a reference resolves to multiple plausible artifacts without structured choices, live behavior contradicts the spec, work expands into model discovery, or repeated repairs fail.

**Permanent records:** V1 spec, ADR 0018, and tests — record accepted repository forms, canonicalization, the separate metadata and download credentials, and job-result behavior. Supersede ADR 0018 with a new ADR if the credential architecture changes.

**Status:** done

- [x] An exact public or authorized protected Hugging Face reference yields an inspectable installation job, with both credentials required for a protected repository.
- [x] An ambiguous or invalid reference does not silently select a different artifact.
- [x] Bediz does not persist or print either token, while InvokeAI's accepted login and temporary install-marker lifecycle is documented; an inconclusive mutation is not retried.

Verification: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed. A live public `PhoenixGS/sd-test-model-lora` installation against local InvokeAI 6.14.1 completed with an inspectable job and model key; the test model was removed afterward. Private-source mutation was not run because no authorized disposable protected fixture was available, so private-source compatibility is supported by transport fixtures only and is not claimed as live verified.
