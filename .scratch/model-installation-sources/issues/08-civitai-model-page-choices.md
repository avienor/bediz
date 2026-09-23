# 08: Return version choices for a Civitai model page

**What to build:** When `source.type: civitai` has a Civitai model page URL containing a positive model ID and no `modelVersionId`, return `selection_required` (exit 3) with `kind: civitai_version`, a numeric model-ID selector, and candidates sorted by numeric version ID; each candidate has integer `id` and `name`. Never select the newest or first version, even if there is only one. The caller submits a chosen decimal version ID as a new `source.reference` through the resolver from ticket 07. This remains source resolution rather than model discovery or recommendation.

**Blocked by:** 07: Resolve an exact Civitai version to one artifact.

**Execution route:** `worker + independent review` — version-choice behavior is deterministic and can be checked against fixed metadata and the accepted ADR.

**Verification gate:** Metadata fixtures prove that a multi-version page returns complete version choices with integer IDs, names, and numeric ordering; a single-version model page still requires an explicit version choice; no install mutation occurs before that choice; and a chosen version ID can be resubmitted through ticket 07. Public-interface tests assert V1 JSON envelope, `selection_required` code, exit status 3, and no leaked token or raw source URL. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`; compare a current model-page metadata response when available and record if live source verification was unavailable.

**Review gate:** After the worker leaves this ticket open as awaiting independent review, a different reviewer starts from the ticket, V1 spec, ADR 0018, worker evidence, and fixed base/head diff. Before fixes, review spec fidelity and repository standards; independently derive the expected version choices from the fixture or live metadata and check that no installation was submitted. Missing choices, implicit newest/first selection, unstable identifiers, mutation before choice, or a broken V1 result contract blocks acceptance. Return findings to implementation, rerun affected checks, and review the revised diff before completion.

**Escalate when:** Metadata does not provide stable exact version identifiers, live behavior contradicts the V1 spec or ADR 0018, the slice grows into ranking or recommendation, or repeated repairs fail.

**Permanent records:** V1 spec, ADR 0018, and tests — define the version-candidate result shape and resubmission behavior. Supersede ADR 0018 with a new ADR if its decision changes.

**Status:** awaiting-review

- [x] A Civitai model page yields version candidates and no install job, even when only one version is listed.
- [x] Candidate identifiers can be submitted as exact version references through ticket 07.
- [x] No version is selected by recency or list order.

Worker evidence: fixture verification passed with `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. Model-page fixtures cover multi-version numeric ordering with and without a slug segment, a single-version page that still returns a choice, `file_id` rejected before metadata, malformed model metadata, and CLI resubmission of a chosen version ID through the ticket-07 resolver. A current Civitai model metadata response for model 827184 listed 17 versions; the CLI returned all 17 as `civitai_version` choices sorted from 925049 to 2883731 with exit 3 and no InvokeAI request. Resubmitting chosen version 2514310 with an out-of-version `file_id` against local InvokeAI 6.14.1 reached the exact-version resolver and was rejected before mutation; the install job list held 8 jobs before and after.

A supplied `--token-stdin` token is also sent as a Bearer header to Civitai's HTTPS model-metadata endpoint, under the same no-redirect boundary as version metadata; V1 section 14 records it. The fixed-diff self review left one open question for the independent reviewer: candidate names are echoed from Civitai metadata without a length cap.
