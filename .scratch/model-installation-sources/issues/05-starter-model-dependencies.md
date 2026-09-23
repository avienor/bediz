# 05: Install a starter model and expose dependency jobs

**What to build:** `models install` accepts `source.type: starter` with an exact `source.reference` matching the `source` identifier in InvokeAI's starter catalog, rather than a display name that may be shared. Traverse dependencies returned by the catalog in order, then the starter. First skip the selected starter or any returned dependency marked installed by the catalog, even if its source form would be unsupported for a new installation. Preflight every remaining entry: a source that cannot be normalized as the intended artifact under the accepted URL and Hugging Face rules, including unsupported variant/subfolder or protected references without the required authentication inputs, returns `unsupported_capability` before any job is submitted. The result's `jobs` array contains only submitted, separately observable jobs with `job_id`, `status`, `source_type: starter`, `role` (`starter` or `dependency`), and zero-based `dependency_index` into the returned dependency list where applicable; a `skipped` array identifies already-installed entries present in the returned catalog by role, index, and `reason: already_installed` without echoing source URLs. If all returned entries are installed, success has no jobs and identifies every returned skip. InvokeAI 6.14.1 removes installed dependencies from its returned list; Bediz cannot report entries it does not receive. If a later submission is conclusively rejected, return `invokeai_operation_failed` with already accepted jobs and skips in structured details; if its response is inconclusive, return `outcome_unknown` with those accepted jobs, skips, and the uncertain role/index. Never replay the uncertain submission. A concurrent 409 is a conclusive conflict requiring reinspection, not an inferred skip. Bediz does not infer dependencies for non-starter sources or hide a partial submission behind a success result.

**Blocked by:** 04: Install a model from a Hugging Face reference.

**Execution route:** `frontier-owned` — scheduling several state-changing jobs and reporting a partially accepted set requires explicit judgment about uncertain outcomes.

**Verification gate:** A catalog fixture with duplicate display names, declared dependencies, mixed installed/missing entries, a plain Hugging Face repository ID colliding with a server-local path, and unsupported variant/subfolder sources proves exact source selection, deterministic traversal, separately returned job IDs, explicit skips, all-installed success, no guessed components, and rejection of any unsafe missing dependency before mutation. Include an all-installed catalog whose sources use unsupported forms; it succeeds with only skips. Mutation-failure tests distinguish rejected jobs, concurrent 409, and inconclusive submissions while preserving accepted IDs and proving no automatic resubmission. `models status` can inspect every returned ID in the current job registry, and `doctor` advertises starter installation only after its requirements are tested. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`; use a safe local InvokeAI 6.14.1 starter fixture if available and report when live dependency installation is unavailable.

**Review gate:** Not required by this route.

**Escalate when:** The live starter catalog does not identify dependencies reliably, InvokeAI installs dependencies implicitly in conflict with the planned job association, a partial failure cannot be represented truthfully, the slice expands into arbitrary dependency inference, or repeated repairs fail.

**Permanent records:** V1 spec and tests — pin exact starter selection, dependency-job association, and partial-outcome semantics. No new terminology or ADR is planned.

**Status:** done

- [x] One exact starter source submits missing catalog entries as individually inspectable jobs and reports returned installed entries as skips.
- [x] Duplicate display names do not cause an arbitrary starter selection.
- [x] Partial or unknown outcomes preserve accepted job identifiers and do not replay a mutation.

## Comments

InvokeAI 6.14.1's live `GET /api/v2/models/starter_models` removes installed dependencies from each returned `dependencies` list instead of marking them installed. The user chose to report only entries actually present in that response. The V1 specification and feature spec now pin that limit and define dependency indexes against the returned list. The user also chose to reject a token-bearing starter request before mutation when the entries to install span multiple URL origins, so a single source token cannot be forwarded to unrelated hosts.

Verification passed: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. Against local InvokeAI 6.14.1, an already-installed Anima starter returned an empty `jobs` array and one starter skip; `doctor` reported the starter capability compatible. A live dependency download was not attempted because the available missing catalog entries require substantial downloads or unsupported source forms. The fixed-diff standards and spec reviews found documentation and Hugging Face URL preflight gaps; those were corrected before completion.
