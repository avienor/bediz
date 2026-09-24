# 08: Models scan

**What to build:** An agent can see which model files exist in a folder on the InvokeAI server before installing them from a path. `bediz models scan --path SERVER_PATH` returns the model paths found there and whether each one is already installed. It installs nothing.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. It is read-only, and its output is a small allowlisted projection.

**Verification gate:** CLI-seam tests cover a missing or non-absolute path (exit 2, no network request), success sorted by path, a folder that InvokeAI rejects (a conclusive error), and a response with extra backend fields (not leaked). The request accepts both flags and a Request Document. `doctor` registers scan as read-only inspection. A live scan of a models folder on 6.14.1 is reported, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §14 and this ticket. Independently confirm that there is no mutation and that the output is allowlisted. A mutation or a leaked field blocks acceptance.

**Escalate when:** The 6.14.1 scan endpoint changes server state, or it does not report installed status.

**Permanent records:** V1 spec §14 records the scan contract, including the rule that the path is resolved on the server as in `models install` path sources. Tests record it.

**Status:** ready-for-agent

## Accepted behavior

- The request has `path`, an absolute path in the InvokeAI server's filesystem namespace. Bediz does not inspect it locally.
- The result data is `{"models":[{"path":..., "installed": bool}]}`, sorted by path.
- The expected 6.14.1 endpoint is the model manager scan-folder GET under `/api/v2/models/`. Confirm it before coding.

- [ ] Scan is read-only and allowlisted
- [ ] Spec §14 is updated
