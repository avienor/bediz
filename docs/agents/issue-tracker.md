# Issue tracker: Local Markdown

Issues and specs for this repo live as Markdown files in `.scratch/`.

## Conventions

- One feature per directory: `.scratch/<feature-slug>/`
- The spec is `.scratch/<feature-slug>/spec.md`
- Implementation issues are one file per ticket at `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01`, never a single combined tickets file
- Workflow state is recorded as a `Status:` line near the top when the publishing skill uses one
- Comments and conversation history append to the bottom of the file under a `## Comments` heading
- `.scratch/backlog.md` lists candidate work that has no feature directory yet; start a feature from an entry there and remove the entry when the feature ships or is rejected

## When a skill says "publish to the issue tracker"

Create a new file under `.scratch/<feature-slug>/`, creating the directory if needed.

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. The user will normally pass the path or issue number directly.
