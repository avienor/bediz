# 05: Modernize queue page and image collection without changing results

**What to build:** Apply the Modern Go Guidelines `min_max` and `slices_sorted` rules to queue inspection. Queue pages remain bounded to the requested window, retain the item-ID order returned for that page, and hydrate no unrequested summaries. Queue output image names remain unique and lexicographically sorted before image inspection; missing historical images continue to be omitted without failing the queue item.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — the implementation is small and fully testable, while array ordering and bounded hydration are externally visible behavior.

**Verification gate:** Tests exercise a full page, a partial final page, an offset at or beyond the available IDs, reversed and duplicate output image names, and a missing historical image. Request counts prove only the selected summary page is hydrated. All project-defined Go checks pass.

**Escalate when:** The refactor changes queue item order, image order, duplicate handling, request count, or missing-image behavior. A desired non-lexicographic image order or different pagination contract requires a separate V1 decision.

**Status:** done

- [x] Page-end selection uses the standard minimum operation without changing early-return or slice-bound behavior.
- [x] Unique image names are collected and sorted from the iterator-backed set in one deterministic operation.
- [x] Bounded hydration and missing historical image behavior remain unchanged.
- [x] Focused queue tests and all repository verification commands pass.

## Comments

Implementation (uncommitted diff against `ce3fa23`):

- `internal/queue/queue.go` (`List`): `end := min(request.Offset+request.Limit, len(ids.ItemIDs))` replaces the manual `if end > len(ids.ItemIDs) { end = len(ids.ItemIDs) }` clamp. The `request.Offset >= len(ids.ItemIDs)` early return and the `ids.ItemIDs[request.Offset:end]` bound are untouched; `Offset >= 0` and `Limit` in `1..100` are validated above, so the addition cannot overflow.
- `internal/queue/queue.go` (`Get`): unique output image names are collected into a `map[string]struct{}` set while the existing filter skips non-`image_output` records and empty names, then iterated with `slices.Sorted(maps.Keys(imageNames))`. The manual seen-map plus `sort.Strings` pair is gone and `sort` is dropped for `maps`/`slices`. Set plus sorted keys produce the identical deduplicated lexicographic sequence; `Item.Images` is still initialized separately, so the nil-versus-empty difference of an empty sorted result is unobservable.

Tests (CLI seam only — argv, stdout envelope, exit status, and observed request traffic):

- `TestQueueListKeepsFinalPageWithinAvailableItemIDs` — offset 2, limit 5 over IDs `[9 8 7]`: page stays within available IDs (`items = [7]`, `total = 3`) and exactly one summary request hydrates `[7]`.
- `TestQueueListSkipsHydrationWhenOffsetIsPastAvailableItemIDs` — offsets 3 (at the available count) and 10 (beyond it): exit 0, empty `items`, and zero summary requests; the summaries endpoint fails the test if it is reached at all.
- `TestQueueGetCollectsUniqueOutputImageNamesInLexicographicOrder` — results carry `zulu.png`, `alpha.png` twice, and an `integer_output`: asserts image requests are exactly `[alpha.png zulu.png]` and the envelope `images` array matches. Random map iteration makes the ordering assertion the real invariant.
- `TestQueueListJSONReturnsOnlyRequestedSummaryPage` gained a summary-request counter asserting exactly one hydration request for the selected page (its existing body assertion already pinned the hydrated IDs).
- A full page and the missing historical image remain covered by `TestQueueListPreservesNewestFirstItemIDOrder` and `TestQueueGetSucceedsWhenHistoricalOutputImageIsMissing`.

Mutation evidence (in-place mutations, reverted):

- Clamp removed (`end := request.Offset + request.Limit`): `TestQueueListKeepsFinalPageWithinAvailableItemIDs` panics with `slice bounds out of range`.
- Early return removed, min clamp kept: the at-count subtest fails with `summary requests = 1`, exit 6 (`invokeai_operation_failed`); the beyond subtest panics with `slice bounds out of range [10:3]`.
- Dedupe dropped (collect a list, then sort): request order `[alpha.png alpha.png zulu.png]`.
- Sort reversed: request order `[zulu.png alpha.png]`.

Live verification against the supported local InvokeAI baseline (6.14.1 at `127.0.0.1:9090`): old and new binaries built from the same tree (only `internal/queue/queue.go` swapped) produced byte-identical exit status, stdout, and stderr for `queue list` (defaults, `--offset 2 --limit 5`, `--offset 4 --limit 2`, `--offset 9 --limit 2`, `--limit 100`) and `queue get` for items 1-4. Item 4 returned five output images; independent `jq` checks confirm the array equals its own sort and has no duplicates.

Verification (Go 1.27.1, module `go 1.27.0`, fresh `-count=1` runs):

```text
go test -count=1 ./...        ok (all packages)
go test -race -count=1 ./...  ok (all packages)
go vet ./...                  clean
go mod verify                 all modules verified
```

Independent review (two axes, uncommitted diff):

- Spec axis: all four checkboxes and every gate scenario verified against the diff; no missing, partial, or unrequested behavior; bounded hydration, item order, and missing-image behavior unchanged; the added counter is exactly the gate's request-count requirement.
- Standards axis: no hard violations; `min` and `slices.Sorted(maps.Keys(...))` are faithful applications of `min_max` and `slices_sorted` with unchanged results. Three judgement calls: (1) the wire-order assertion on image fetches — kept, because the ticket pins "lexicographically sorted before image inspection" and names image order as escalation-worthy, so the envelope assertion alone would not defend the accepted behavior; (2) duplicated queue-list test scaffolding — kept, matching the file's established inline-fixture convention and AGENTS.md's vertical-slice rule; (3) new test JSON code uses `encoding/json` rather than the `json_v2` guideline — kept, because the file and the production seam are uniformly `encoding/json` and a second JSON convention inside one test file conflicts with the repo's single-convention rule, while the guideline itself says to leave existing `encoding/json` code unchanged.
