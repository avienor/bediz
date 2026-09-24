---
status: accepted
---

# Build releases reproducibly with a repository-local command

Release archives are built by `go run ./tools/release vX.Y.Z`, a command in this repository that uses only the Go toolchain and the Go standard library's archive and compression packages. No third-party release tool takes part. Anyone with the pinned toolchain can rebuild a published version and compare the result byte for byte with `SHA256SUMS`.

The command builds only an existing valid version tag that points to the checked-out commit, from a clean working tree, with the exact Go toolchain named by the `toolchain` directive in `go.mod`. The toolchain is pinned to one patch version because the compiler version is recorded in each binary and changes its bytes. A clean tree keeps `vcs.modified=true` out of the build information. The binaries are `CGO_ENABLED=0` and `-trimpath` builds with fixed microarchitecture levels, linked with `-s -w` to leave out the symbol table and DWARF data. The release version, commit, and commit date are injected with ldflags. The command rejects a binary whose build information shows settings from the environment, such as build tags or experiments.

Archive entries have fixed contents and metadata. Tar archives use the USTAR format with owner and group `0`, empty owner and group names, mode `0755` for the binary and `0644` for other files, and the commit time. Gzip headers have no file name, a zero modification time, and OS byte 255. Zip entries use the commit time in MS-DOS format, fixed modes, and no extra fields. The MS-DOS format has two-second precision, so an odd-second commit time is stored one second earlier. The extended-timestamp field that could hold it is left out, so each entry has the same fixed layout.

The considered alternatives were GoReleaser and a shell script around `tar` and `zip`. GoReleaser adds a third-party tool and its configuration to the trust and reproduction path. GNU and BSD `tar`, `gzip`, and `zip` differ in flags and defaults, so a shell script would give different bytes on Linux and macOS.

## Consequences

Upgrading Go for releases is an explicit `go.mod` change. A `go install` user with an older local Go downloads the pinned toolchain through the default `GOTOOLCHAIN=auto` setting. A newer local toolchain builds normally. The release command must run on a host that matches a release target, because it runs the host's binary from the finished archive to check the reported version, commit, and date. Releases are published only by pushing a `v*` tag, which runs `.github/workflows/release.yml`. The workflow runs the same command, so published archives and local rebuilds come from one code path. A build job with read-only repository access checks the tag, runs the verification commands, and builds the archives. A separate publish job, the only one with `contents: write`, uploads exactly those archives and `SHA256SUMS` to a GitHub Release for the tag, and marks a tag with a prerelease suffix as a prerelease. The command also refuses a version that differs from `metadata.bediz-version` in `skills/bediz/SKILL.md`, so every released tag carries a skill that declares its version.
