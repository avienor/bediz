# 03: Publish tagged releases to GitHub

**What to build:** Pushing a tag that matches `v*` runs a GitHub Actions workflow. It rejects a tag that is not a valid version name (ticket 01) and a tag whose version differs from the `metadata.bediz-version` field of `skills/bediz/SKILL.md` (ticket 02). It then runs the four verification commands with the pinned toolchain, builds the archives and `SHA256SUMS` with the ticket 01 command, and publishes them as a GitHub Release for that tag. A tag with a prerelease suffix, such as `v1.0.0-rc.1`, is published as a prerelease. The workflow publishes nothing when verification fails.

**Blocked by:** 01 (Reproducible release build), 02 (Canonical agent skill).

**Execution route:** `frontier-owned`. Publication is outward-facing and hard to reverse, and it depends on repository settings and permissions.

**Verification gate:** A prerelease tag produces a GitHub Release whose assets match the ticket 01 names. The downloaded assets pass `sha256sum -c SHA256SUMS`, and their checksums equal those of a local ticket 01 build of the same tag. A tag whose version differs from the skill's declared version publishes no release, and neither does a deliberately failing verification step; test this on a fork or a throwaway tag. The workflow grants only the permissions it needs to create a release.

**Escalate when:** The repository is still private, the workflow needs secrets beyond the default token, CI output differs from a local build, or a published release has to be deleted or rewritten.

**Permanent records:** The ticket 01 ADR records publication through tagged GitHub Releases. README records the release process for maintainers.

**Status:** ready-for-agent

## Accepted behavior

- The trigger is only a `v*` tag push. Merges to `master` publish nothing.
- Release assets are exactly the ticket 01 output.
- Every published tag contains the skill, and the skill declares that tag's version.
- Pushing a real tag is the user's decision. The implementer asks before pushing any tag to the main repository.

- [ ] A tag push publishes verified archives and `SHA256SUMS`
- [ ] Failed verification or a skill version mismatch publishes nothing
- [ ] CI checksums match a local build
