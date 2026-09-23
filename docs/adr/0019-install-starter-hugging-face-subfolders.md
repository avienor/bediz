---
status: accepted
---

# Install starter Hugging Face subfolders through the catalog source

This amends ADR-0018 for InvokeAI starter catalog entries only. A selected starter or returned dependency may use one `org/repo::path` source, where `path` is a relative folder or file in the repository. Bediz preflights every entry it would install, then passes the exact catalog string once to InvokeAI's tested generic POST installer. User-supplied Hugging Face subfolders, variants, and `+` multi-subfolder sources remain unsupported. The public Installation Request Document and flags do not change.

Bediz verifies the repository part as a plain ID and rejects empty, absolute, dot-segment, backslash, colon, and multi-subfolder paths. It checks existence through anonymous Hugging Face tree listings: the parent listing identifies the exact source path as a file or directory, and a directory requires a file in its first recursive listing page. Parent listings are followed through pagination. A missing, denied, malformed, or unavailable tree result fails the entire starter preflight before any job is submitted. InvokeAI's repository metadata does not establish existence for Diffusers-layout repositories because the tested 6.14.1 endpoint can return `urls: null` for them.

Public repositories need no credential. When the anonymous repository check cannot verify public access, Bediz requires `auth huggingface status` to report `valid` before submitting any job. InvokeAI uses its own stored Hugging Face login for the protected subfolder download. Bediz neither reads nor sends that token. A starter request with `--token-stdin` and any subfolder entry to install fails before mutation because that source has no URL origin.

InvokeAI 6.14.1 checks whether the catalog string names a server-local path before parsing it as a Hugging Face source. Bediz cannot inspect the server filesystem and accepts this residual collision risk. When InvokeAI has a stored Hugging Face login, its 6.14.1 installer attaches that token to Hugging Face sources even for public repositories. It can write the token to a plaintext temporary install marker during either public or protected downloads; normal completion removes the marker, while a paused or interrupted download can retain it. Bediz's anonymous preflight requests never carry the token. This is the same server-side credential lifecycle accepted in ADR-0018. A different source parser or credential lifecycle requires a new compatibility decision.
