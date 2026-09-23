# 03: Install starter entries that use Hugging Face subfolder sources

**What to build:** `models install --source-type starter` supports catalog entries whose InvokeAI `source` uses the single-subfolder form `org/repo::path`. This applies to the selected starter and to each returned dependency. `path` is a folder or a single file inside the repository, for example `InvokeAI/t5-v1_1-xxl::bnb_llm_int8` or `InvokeAI/flux_schnell::transformer/bnb_nf4/flux1-schnell-bnb_nf4.safetensors`. After this ticket, the `FLUX.1 schnell (quantized)` and `FLUX.1 dev (quantized)` starters install through Bediz together with their T5 encoder, FLUX VAE, and CLIP Embed dependencies.

- **Scope:** Starter entries only. The public Installation Request Document, the flags, and the `huggingface` source type are unchanged. A user-supplied `huggingface` reference with `::` is still `invalid_request`.
- **Submission:** Bediz submits the exact catalog string to InvokeAI's tested generic POST installer, once. There is no HTTPS URL equivalent for a subfolder, so the plain-repository URL conversion does not apply.
- **Known residual risk:** InvokeAI 6.14.1 checks for a server-local path named by the string before treating it as Hugging Face. Bediz cannot inspect the server filesystem and does not try. This risk is recorded, not mitigated.
- **Shape checks before any mutation:** Each entry must have a valid plain `org/repo` part and a non-empty relative subfolder path. The path must not have a leading slash, an empty segment, `.` or `..` segments, backslashes, or a `:`. A variant (`org/repo:variant...`) or a multi-subfolder `+` form is `unsupported_capability`; those stay deferred.
- **Existence check before any mutation:** InvokeAI's repository metadata cannot be used for this, because InvokeAI 6.14.1 returns `urls: null` for repositories it recognizes as Diffusers. The T5, CLIP Embed, and `black-forest-labs/FLUX.1-schnell` repositories are all in that group. Instead, Bediz uses Hugging Face's anonymous tree API without a token, the same way it already runs the anonymous public-access check. The API lists folders only. `/tree/main/{path}` returns `404` for a file path, even an existing one. The check therefore has two steps:
  1. List the source path's parent folder without recursion (`/tree/main` for a path at the repository root). Follow its pagination until the entry whose `path` equals the source path exactly is found.
  2. If that entry has `type: file`, the source exists. If it has `type: directory`, list the folder with `?recursive=true` and require at least one entry with `type: file`. The first page is enough.

  Whether a path is a file or a folder is decided only by the entry's `type`, never from a file extension. A missing entry, an entry of another type, an empty folder, a `404`, a denied response, a malformed body, or a transport failure returns `unsupported_capability` before mutation. Existence is never assumed. Evidence from 2026-09-23:
  - a direct file query returned `404` for the FLUX main file and for `ae.safetensors`, while their parent listings showed both with `type: file`;
  - `InvokeAI/t5-v1_1-xxl` `bnb_llm_int8` lists only subfolders without recursion, and lists files with `recursive=true`;
  - the gated `black-forest-labs/FLUX.1-schnell` repository was listed anonymously.
- **Access and credentials:**
  - Access is checked with the same anonymous Hugging Face check used today.
  - A public repository needs no credential.
  - A repository that is not verifiably public is installed only when `auth huggingface status` reports `valid` before mutation. Otherwise the result is `unsupported_capability`, the same as other starter entries that lack authentication inputs. On this path InvokeAI uses its own stored login token. Bediz sends no token and never reads the stored one.
  - `--token-stdin` together with a starter request that would install any subfolder entry is `unsupported_capability` before mutation, because a subfolder source has no URL origin.
- **Unchanged starter semantics:** catalog order, `skipped` entries marked `already_installed`, dependency indexes, all-or-nothing preflight, partial `invokeai_operation_failed` and `outcome_unknown` details, no replay, and no echo of source references.
- **Records:** Record the accepted boundary in a new ADR that amends ADR 0018's rule that subfolder forms are unsupported. Mark ADR 0018 as amended by it.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned` — the slice changes an accepted source-resolution and credential boundary and needs a new ADR. It also accepts a residual server-path risk, and its live effect (downloads from gated repositories that write InvokeAI's stored token to a temporary marker) cannot be fully checked by automated tests.

**Verification gate:**
- Public-seam tests with catalog fixtures cover:
  - folder and single-file subfolder entries as starter and dependency;
  - the exact string submitted once per entry;
  - rejected shapes (variant, `+`, leading slash, `..`, empty segment, backslash, extra `:`) failing before any mutation;
  - a single-file source found in its parent listing (including at the repository root);
  - a folder source whose files exist only in nested subfolders;
  - an entry found only on a later page of the parent listing;
  - a source path absent from its parent listing, an empty folder, and `404`, denied, malformed, and transport-failure tree responses;
  - a Diffusers repository whose InvokeAI metadata has `urls: null` passing through the tree API;
  - no token sent to the tree API;
  - a public repository with no credential;
  - a gated repository with `valid`, `invalid`, and `unknown` login status (only `valid` proceeds);
  - `--token-stdin` combined with a subfolder entry;
  - a mixed starter where a later dependency fails preflight, so no job is submitted;
  - already-installed subfolder entries skipped without shape checks;
  - partial rejection and an inconclusive submission preserving accepted jobs, with no replay;
  - no source reference, token, or backend error text in output.
- A regression test shows that a user-supplied `huggingface` source with `::` is still `invalid_request`.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live against the local InvokeAI 6.14.1 baseline, following the repository's live-verification guide, with the user-consented models:
  - after the user logs in to InvokeAI's Hugging Face integration and accepts the `black-forest-labs/FLUX.1-schnell` terms, `bediz models install --source-type starter` for `FLUX.1 schnell (quantized)` submits the dependency and starter jobs, and `models status` shows each job completed with a Model Key;
  - a following install of `FLUX.1 dev (quantized)` reports the shared dependencies as `skipped` (or, if InvokeAI omits them from its returned list, records that);
  - `doctor --json` still reports starter installation as compatible.
- Record whether InvokeAI wrote the stored login token to the temporary marker during the gated download and whether the marker was removed on completion.
- Report any live step that was unavailable.

**Review gate:** Not required by this route.

**Escalate when:**
- InvokeAI resolves a catalog `::` string differently from the recorded HF subfolder behavior, for example by treating it as a local path on the baseline.
- The anonymous Hugging Face tree API stops listing paths in a gated or public repository, or its response shape changes.
- A gated download does not use the stored login, or it needs a credential path this ticket does not allow.
- The marker keeps the login token after normal completion.
- V1 families need a variant or `+` form.
- The slice starts expanding into arbitrary user-supplied subfolder sources.
- Repeated repair loops fail.

**Permanent records:**
- The repository's live-verification guide: remove its note that `::` starter entries fail preflight.
- New ADR (amending ADR 0018): accepted starter `::` subfolder support, the accepted residual server-path risk, reliance on InvokeAI's stored login for protected subfolder entries, that login token's temporary-marker lifecycle, and the continued deferral of variant, `+`, and user-supplied subfolder forms.
- V1 spec §14: starter subfolder preflight, credential behavior, and failure paths. §20: the stored-login marker boundary.
- Tests.
- No new terminology: a Model Source Reference and Source Resolution already cover it.

**Status:** complete

- [x] Starter entries with a single-subfolder `org/repo::path` source install through one exact submission each, after shape, existence, and access checks, with no job submitted when any entry fails preflight.
- [x] Entries in protected repositories rely only on InvokeAI's stored Hugging Face login; Bediz never sends or reads a token for them, and the resulting credential boundary is recorded.
- [x] The FLUX.1 schnell and dev quantized starters and their dependencies install live through Bediz, or the unavailable live step is reported.

## Comments

### 2026-09-23: implementation and live verification

- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed after the public-seam tests were extended to both folder/file role combinations and a Diffusers `urls: null` fixture.
- `bediz auth huggingface status --url http://127.0.0.1:9090 --json` reported `valid`. `bediz doctor --url http://127.0.0.1:9090 --json` reported `models.install` / `starter` compatible.
- `bediz models install --source-type starter --source 'InvokeAI/flux_schnell::transformer/bnb_nf4/flux1-schnell-bnb_nf4.safetensors' --url http://127.0.0.1:9090 --json` submitted jobs 10 (T5), 11 (FLUX VAE), 12 (CLIP Embed), and 13 (schnell main) in catalog order. Job 10 ended in `error` after an incomplete network read; jobs 11 and 12 completed with Model Keys. The model inventory had no T5 key after job 10 failed.
- After that conclusive failure and inventory check, `bediz models install --source-type starter --source 'InvokeAI/t5-v1_1-xxl::bnb_llm_int8' --url http://127.0.0.1:9090 --json` submitted a new T5 job 14. Jobs 13 and 14 completed with Model Keys. `models list --json` confirmed all four schnell components in the inventory: T5, FLUX VAE, CLIP Embed, and the quantized schnell main.
- While downloads were active, temporary InvokeAI install markers contained its stored login token (checked as a Boolean without printing its value). Markers for the failed T5 job and completed jobs 11–14 were removed; after job 14 completed, the marker count was zero. Inspection of the installed InvokeAI 6.14.1 source showed that it attaches a stored login token to public Hugging Face sources as well as protected ones; ADR-0019 and V1 §20 now record this broader server-side behavior.
- `bediz models install --source-type starter --source 'InvokeAI/flux_dev::transformer/bnb_nf4/flux1-dev-bnb_nf4.safetensors' --url http://127.0.0.1:9090 --json` submitted job 15 for the dev main only. InvokeAI omitted the already installed shared dependencies from its returned catalog list, so `skipped` was empty as the V1 specification allows. Job 15 completed with a Model Key; `models list --json` confirmed the quantized dev main in the inventory. After completion, there were zero temporary install markers, and `doctor --json` still reported `models.install` / `starter` compatible.
