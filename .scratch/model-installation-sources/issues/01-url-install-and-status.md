# 01: Install a model from an exact URL and inspect its job

**What to build:** `models install` accepts an exact public HTTP(S) artifact URL through matching `--source-type url` and `--source` flags or a schema-version-1 Request Document whose `source` object contains `type: url` and `reference`. User-supplied direct artifact URLs with userinfo, any query, or a fragment fail validation before mutation because InvokeAI logs and records the source. Both input forms compile to the same typed operation; operation flags do not mix with `--request`. InvokeAI owns the installation job. Submit through the tested generic POST installation API, never a mutating GET. The install result contains a `jobs` array with one record carrying integer `job_id`, normalized `status`, `source_type: url`, and `role: requested`; it does not echo the source URL. `models status` accepts a non-negative integer `job_id` (including zero) through `--job-id` or a Request Document and returns an allowlisted projection of the job currently held under that ID: ID, normalized status, optional progress bytes, and an exact installed Model Key only when verified from the completed backend record. InvokeAI 6.14.1 can remove or reuse IDs after restart, so Bediz must not present an ID as a durable receipt; uncertain outcomes require inspection of the current model inventory and job list before another submission. Raw backend source, access token, error text, and traceback are excluded. The JSON path uses one V1 Result Envelope per invocation. Register installation for the tested InvokeAI range and status as compatible read-only inspection in `doctor`; reject installation on an untested version. Scope excludes scan, deletion, and other source types.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned` — this slice fixes the first public installation request, job-result, and status contract across an asynchronous mutation.

**Verification gate:** Public-interface tests exercise flags and Request Documents, unknown and inapplicable fields, malformed URLs, userinfo/query/fragment rejection before mutation, job ID zero and other exact IDs, known and unknown status values, structured failures, stdout/stderr/exit status, supported versus untested version behavior, and `doctor` registration. Test a backend job containing `source.access_token`, `error`, and `error_traceback`; none of those raw values may reach output. A transport test proves the generic POST mutation is sent once and a lost response yields `outcome_unknown` with inventory/job-list inspection guidance. A restart/reused-ID fixture proves status is not described as a durable identity. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. Check one safe, test-controlled installation and subsequent status against local InvokeAI 6.14.1 if a suitable valid artifact is available; otherwise record why live installation could not be exercised.

**Review gate:** Not required by this route.

**Escalate when:** The live job response or state sequence conflicts with the accepted V1 contract, the source needs a different public shape, implementation grows into unrelated model management, or repeated repairs fail for the same underlying reason.

**Permanent records:** V1 spec and tests — record the exact install/status request and result fields, job identity, and failure behavior. Existing terminology in `CONTEXT.md` and accepted ADRs remains sufficient unless evidence contradicts it.

**Status:** done

- [x] `models install` turns an exact public URL without userinfo, query, or fragment into one observable InvokeAI installation job, with matching flag and Request Document behavior and no duplicate submission.
- [x] `models status` reports the exact job's status through a safe projection without exposing the backend source, credentials, raw errors, or traceback, and does not guess that a missing job succeeded.
- [x] JSON output and `doctor` reflect only the implemented, tested capability.
