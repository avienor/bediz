# 01: Reproducible release build with version reporting and checksums

**What to build:** One repository-local command builds a given version, such as `v1.0.0`, into a release directory. Before building, it refuses to run unless all of these hold:

- The version matches `vX.Y.Z` or `vX.Y.Z-<prerelease>`.
- A tag with that exact name exists and points to the checked-out commit.
- The working tree is clean, so the build information never records `vcs.modified=true`.
- The active Go toolchain is exactly the version pinned by the `toolchain` directive in `go.mod`.

After building, it fails unless the extracted native binary's `version --json` reports the requested version and the tagged commit. The directory holds `bediz_<version>_linux_amd64.tar.gz`, `bediz_<version>_darwin_arm64.tar.gz`, `bediz_<version>_windows_amd64.zip`, and `SHA256SUMS`. Each archive holds the binary (`bediz` or `bediz.exe`), `LICENSE`, and `README.md`. Two runs on the same tag, on any machine with the pinned toolchain, produce byte-identical archives. This ticket also adds the Apache-2.0 `LICENSE` file that ADR-0004 and spec §20 require.

`bediz version`, `bediz version --json`, and the `bediz` section of `bediz doctor` report the version the binary was built as:

- A release build reports the version, commit, and commit date injected with ldflags.
- Without ldflags, Bediz reads the Go build information. A `go install github.com/avienor/bediz/cmd/bediz@vX.Y.Z` build reports `vX.Y.Z`, plus the VCS revision and time when the build information carries them. The `(devel)` module version is treated as absent.
- A build without either source reports `dev`, and it omits commit and date instead of reporting `unknown`.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The behavior is fully specified and reversible, and every property can be checked by machine.

**Verification gate:** Tests at the CLI seam, or through the version module's public interface, cover the three version sources and their precedence. Two clean runs of the release command on the same tag produce identical `SHA256SUMS`, and `sha256sum -c SHA256SUMS` passes. The extracted Linux binary reports the given version, commit, and date in `version --json`. `go version -m` on each binary shows the expected GOOS and GOARCH, `CGO_ENABLED=0`, and `-trimpath`. Archive listings show exactly the three files, with fixed timestamps and no local user, group, or path data. Tests or scripted checks show that the command refuses an invalid version name, a tag that points elsewhere, a dirty tree, and a different Go toolchain. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** A light review focused on the riskiest claims, not a full diff read. The reviewer runs the release command on the same tag in a separate clean clone and compares its `SHA256SUMS` with the worker's output. The reviewer lists each archive's contents, checks `version --json` on the extracted Linux binary, and tries one refusal case (a dirty tree or a mismatched tag). The following block acceptance: non-reproducible output, a missing refusal check, a local path or build timestamp in a binary or archive, a missing `LICENSE` or target, or changed `version` JSON field names.

**Escalate when:** Pinning the toolchain conflicts with the `go` directive or `go install` users, byte-identical output cannot be achieved with the Go toolchain and standard archive tools, a target needs cgo, or the Go build information cannot provide the module version for a `go install` of a tag.

**Permanent records:** V1 spec §21 records the archive names, their contents, `SHA256SUMS`, and how a `go install` build reports its version. A new ADR records the reproducible repository-local build decision. README records the release command and its preconditions for maintainers. `go.mod` gains the pinned `toolchain` directive. The `LICENSE` file is added, and tests record the version precedence.

**Status:** ready-for-agent

## Accepted behavior

- No third-party release tool. The build uses the Go toolchain and standard archive tools only.
- The Go toolchain is pinned to one exact patch version, because the compiler version is recorded in each binary's build information.
- Archive settings are fixed:
  - tar uses one explicit format, with owner and group `0` and empty names;
  - file modes are `0755` for the binary and `0644` for the other files;
  - every entry's time is the tagged commit's time;
  - gzip headers carry no file name, a zero modification time, and a fixed OS byte;
  - zip entries carry the commit time and fixed modes, with no extra fields;
  - entries appear in one fixed order.
- The version name must be a tag on the checked-out commit. The command never builds an untagged commit as a release.
- The skill is not bundled in the archives.
- The `version` JSON fields stay `version`, `commit`, and `date`, and `version` and `doctor` report the same values.

- [ ] Version sources follow the accepted precedence
- [ ] One command produces the three archives and `SHA256SUMS`
- [ ] Repeated builds are byte-identical
- [ ] The command refuses invalid versions, mismatched tags, dirty trees, and other toolchains
- [ ] `LICENSE` is present in the repository and in every archive
- [ ] Spec §21, the ADR, and README are updated
