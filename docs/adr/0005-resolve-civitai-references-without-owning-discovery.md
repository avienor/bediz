---
status: superseded by ADR-0018
---

# Resolve Civitai references without owning model discovery

Bediz will accept Civitai model-version page URLs as model source references and resolve them through Civitai's model-version metadata to an exact downloadable artifact. Direct download URLs will pass through unchanged. When a reference does not identify a version, or a version contains multiple files without an unambiguous primary artifact, Bediz will return a non-interactive `selection_required` result with structured choices instead of selecting the newest version or guessing a file.

Bediz will not provide Civitai search, ranking, or recommendation in the initial product. A creative agent may use its browser or other research tools for discovery and then pass the selected reference to Bediz for deterministic resolution and installation. Authentication will use the same non-persistent token input as other protected URL installations.

## Consequences

Agents may give Bediz the human-facing Civitai link they found without scraping a download button. Bediz must maintain a small Civitai metadata adapter, but Civitai's discovery experience and content policy remain outside the controller's scope. Model-only links and ambiguous file sets produce explicit selection results rather than hidden defaults.
