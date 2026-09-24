# Canonical agent skill, cross-platform release builds, and checksums

Delivery slice 9 of `docs/spec/v1.md` §23. It delivers §19 (Agent skill), §21 (Release targets), and the remaining §24 V1 acceptance evidence.

## Scope

- Correct Bediz version reporting in release builds and `go install` builds.
- Reproducible release archives for Linux amd64, Windows amd64, and macOS arm64, with SHA256 checksums and the Apache-2.0 license.
- Tag-triggered publication of those archives as a GitHub Release.
- The canonical `skills/bediz/SKILL.md` and a test that keeps it aligned with the CLI.
- Installation documentation for people (README) and for agents (`INSTALLATION.md`).
- The V1 acceptance run: an agent with only the skill completes a live generation through Bediz.

Package-manager formulas, a self-updater, signed installers, additional architectures, and a Bediz command that installs or prints the skill stay out of scope.

## Accepted decisions

The user accepted these decisions on 2026-09-24. Each ticket records the decisions it needs, and the implementing ticket writes them into the V1 spec or an ADR.

1. **The repository will be public.** Tickets proceed as if `github.com/avienor/bediz` is public. `go install github.com/avienor/bediz/cmd/bediz@<version>` and anonymous `npx skills add avienor/bediz` are supported installation paths. Making the repository public is the user's action, and tickets that need it escalate until it has happened.
2. **Version reporting.** A binary built with release ldflags reports those values. Without ldflags, Bediz reads the Go build information: the module version for a `go install` of a tagged version, and the VCS revision and time when present. Otherwise it keeps `dev` and omits unknown commit and date values. The `version` JSON fields stay `version`, `commit`, and `date`.
3. **Release tooling.** A repository-local build script with no third-party release tool. It builds only an existing valid version tag on the checked-out commit, from a clean tree, with the exact Go toolchain pinned in `go.mod`. It produces `CGO_ENABLED=0`, `-trimpath` builds with fixed archive settings, so two builds of the same tag are byte-identical.
4. **Artifact format.** `tar.gz` for Linux and macOS and `zip` for Windows. Each archive holds the binary, `LICENSE`, and `README.md`. One `SHA256SUMS` file covers every archive. The skill is not bundled in the archives.
5. **Publication.** Pushing a `v*` tag runs the verification commands, builds the archives with the same script, and uploads them and `SHA256SUMS` to a GitHub Release. It refuses a tag whose version differs from the skill's declared `metadata.bediz-version`.
6. **Skill distribution.** The repository holds one canonical `skills/bediz/SKILL.md` (ADR-0014). People install it with `npx skills add avienor/bediz`. Users who ask their agent to install Bediz point it at `INSTALLATION.md`. That document has the agent download the matching release archive, verify it against `SHA256SUMS`, put the binary on PATH, run `bediz version` and `bediz doctor`, and install the skill from the same tag as the binary after asking the user whether to install it for the project or globally. The Bediz binary gains no skill command and writes to no agent directory.
7. **Skill and binary versions stay matched.** Both installation documents install the skill from the tag that matches the installed binary, not from `master`. The skill declares its matching version in `metadata.bediz-version`, and the release workflow enforces it.
