---
status: accepted
---

# Store technical profiles locally

Generation profiles will be portable JSON documents stored in Bediz's per-user operating-system configuration directory. V1 will not add project-local or cloud profile resolution. A profile may contain model and component preferences, exact dimensions, applicable generation settings, upscale settings, and its schema version. It will not contain prompts, seeds, source images, output boards, credentials, or other secrets.

Creating a profile whose name already exists will fail unless the caller supplies both `--replace` and `--yes`. Replacement will be atomic. Bediz will not retain profile revision history; profile versioning refers to the JSON schema version, not saved revisions.

## Consequences

Profiles remain deterministic technical presets instead of becoming prompt libraries, job templates, or secret stores. Users can copy and inspect them easily, but must use an external version-control or backup system if they need revision history.
