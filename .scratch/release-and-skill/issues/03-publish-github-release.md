# 03: Publish tagged releases to GitHub

**What to build:** Pushing a tag that matches `v*` runs a GitHub Actions workflow. It rejects a tag that is not a valid version name (ticket 01) and a tag whose version differs from the `metadata.bediz-version` field of `skills/bediz/SKILL.md` (ticket 02). It then runs the four verification commands with the pinned toolchain, builds the archives and `SHA256SUMS` with the ticket 01 command, and publishes them as a GitHub Release for that tag. A tag with a prerelease suffix, such as `v1.0.0-rc.1`, is published as a prerelease. The workflow publishes nothing when verification fails.

**Blocked by:** 01 (Reproducible release build), 02 (Canonical agent skill).

**Execution route:** `frontier-owned`. Publication is outward-facing and hard to reverse, and it depends on repository settings and permissions.

**Verification gate:** A prerelease tag produces a GitHub Release whose assets match the ticket 01 names. The downloaded assets pass `sha256sum -c SHA256SUMS`, and their checksums equal those of a local ticket 01 build of the same tag. A tag whose version differs from the skill's declared version publishes no release, and neither does a deliberately failing verification step; test this on a fork or a throwaway tag. The workflow grants only the permissions it needs to create a release.

**Escalate when:** The repository is still private, the workflow needs secrets beyond the default token, CI output differs from a local build, or a published release has to be deleted or rewritten.

**Permanent records:** The ticket 01 ADR records publication through tagged GitHub Releases. README records the release process for maintainers.

**Status:** awaiting-review

## Accepted behavior

- The trigger is only a `v*` tag push. Merges to `master` publish nothing.
- Release assets are exactly the ticket 01 output.
- Every published tag contains the skill, and the skill declares that tag's version.
- Pushing a real tag is the user's decision. The implementer asks before pushing any tag to the main repository.

- [x] A tag push publishes verified archives and `SHA256SUMS`
- [x] Failed verification or a skill version mismatch publishes nothing
- [x] CI checksums match a local build

## Comments

**2026-09-24, implementation:** The workflow is `.github/workflows/release.yml`.

- **Build job** (`contents: read`): checks out the tag without persisting credentials and reads the `toolchain` directive from `go.mod` and installs that version with `actions/setup-go` (`GOTOOLCHAIN=local`, no cache). It then runs `go run ./tools/release -check <tag>`, the four verification commands, and `go run ./tools/release <tag>`, verifies `sha256sum -c SHA256SUMS`, and uploads `dist/<tag>` as an artifact.
- **Publish job** (`contents: write`, the only job that can write, runs only after a successful build): downloads the artifact, re-checks `SHA256SUMS`, and runs `gh release create <tag> --verify-tag`, adding `--prerelease` when the tag has a `-` suffix.
- **Actions:** pinned to commit SHAs.
- **No extra secrets:** the job needs none beyond the default token.

The release command now refuses a version that differs from `metadata.bediz-version` (`ErrSkillVersionMismatch`), in `Build` and in the new `-check` mode (`release.Check`). A local build therefore enforces the same rule as CI. README, spec §21, and ADR-0021 record publication and the rule.

**Local evidence** (scratch clone with a local-only tag; nothing pushed):
- `-check v1.0.1` was refused with the skill mismatch.
- `-check v1.0.0` passed.
- After `go test ./...`, the tree stayed clean.
- `go run ./tools/release v1.0.0` built all three archives, and `sha256sum -c` passed.
- `actionlint` 1.7.12 reports no problems.

**Observation:** two version tags on one commit make `go build` record the higher tag as the module version, so the build is refused. The skill-version rule prevents this for releases, because a prerelease commit and its final commit must declare different skill versions.

**2026-09-24, live verification on `avienor/bediz`** (user approved; throwaway `v0.0.0-ci.N` tags on detached test commits):

- **Workflow not triggered at first:** a tag whose commit was not in any pushed branch did not start the workflow. After `feature/release-and-skill` was pushed, the same tag did.
- **First run, wrong toolchain:** `actions/setup-go` v7.0.0 resolved `go-version-file: go.mod` to the `go` directive (1.27.0), and `-check` refused the build. Nothing was published. The workflow now reads the `toolchain` directive and passes it as `go-version` (commit 456497f).
- **`v0.0.0-ci.1`** (skill declares `v0.0.0-ci.1`): run 36018125715 succeeded and published a prerelease that is not a draft. Its assets were exactly the three archives and `SHA256SUMS`. The downloaded assets passed `sha256sum -c`, and `SHA256SUMS` was byte-identical to a local build of the same commit and tag.
- **`v0.0.0-ci.2`** (same commit; the skill declares `ci.1`): run 36018635094 failed at the skill version check. The publish job was skipped, and no release was created.
- **`v0.0.0-ci.3`** (skill declares `ci.3`, with a deliberately failing test): run 36018634595 failed at `go test ./...`. The publish job was skipped, and no release was created.
- **Cleanup:** the test release and all three tags were then deleted locally and on GitHub.
