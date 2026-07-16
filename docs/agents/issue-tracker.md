# Issue tracker: GitHub

Issues and specs for this repo live as GitHub issues in **`GSA-TTS/ppp`**. Use the `gh` CLI for all operations. Skills that read from / write to the tracker: `to-tickets`, `to-spec`, `triage`, `implement`, `wayfinder`.

The `origin` remote points at `git@github.com:GSA-TTS/ppp.git`, so `gh` infers the repo automatically inside a clone.

## Conventions

- **Create an issue**: `gh issue create --title "..." --body "..."`. Use a heredoc for multi-line bodies.
- **Read an issue**: `gh issue view <number> --comments`, filtering comments by `jq` and also fetching labels.
- **List issues**: `gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'` with appropriate `--label` and `--state` filters.
- **Comment on an issue**: `gh issue comment <number> --body "..."`
- **Apply / remove labels**: `gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> --comment "..."`

Infer the repo from `git remote -v` — `gh` does this automatically when run inside a clone.

## When a skill says "publish to the issue tracker"

Create a GitHub issue.

## When a skill says "fetch the relevant ticket"

Run `gh issue view <number> --comments`.

## Wayfinding operations

The `wayfinder` skill plans large efforts as a **map** issue with **decision-ticket**
child issues. This repo has GitHub's native **sub-issues** and **issue dependencies**
enabled (verified), so wayfinder uses those — the frontier renders visually in
GitHub's own UI.

- **Map issue:** a normal issue labelled `wayfinder:map`. Its body follows the
  wayfinder map template (Destination / Notes / Decisions so far / Not yet
  specified / Out of scope).
- **Ticket issues:** normal issues, each a **sub-issue** of the map, each carrying
  exactly one type label: `wayfinder:research`, `wayfinder:prototype`,
  `wayfinder:grilling`, or `wayfinder:task`.
- **Create a child as a sub-issue of the map** (needs the child's numeric `id`,
  not its number):
  ```bash
  CHILD_ID=$(gh api repos/GSA-TTS/ppp/issues/<child#> --jq '.id')
  gh api -X POST repos/GSA-TTS/ppp/issues/<map#>/sub_issues -F sub_issue_id="$CHILD_ID"
  ```
- **List a map's sub-issues:**
  ```bash
  gh api repos/GSA-TTS/ppp/issues/<map#>/sub_issues --jq '.[] | {number, title, state}'
  ```
- **Wire a blocking edge** (ticket A is blocked by ticket B; needs B's numeric `id`):
  ```bash
  B_ID=$(gh api repos/GSA-TTS/ppp/issues/<B#> --jq '.id')
  gh api -X POST repos/GSA-TTS/ppp/issues/<A#>/dependencies/blocked_by -F issue_id="$B_ID"
  ```
- **Read what a ticket is blocked by:**
  ```bash
  gh api repos/GSA-TTS/ppp/issues/<A#>/dependencies/blocked_by --jq '.[] | {number, title, state}'
  ```
- **Frontier query** (open, unblocked, unclaimed children of the map): list the
  map's open sub-issues, drop any with an open `blocked_by`, drop any with an
  assignee. A ticket is **claimed** by assigning it: `gh issue edit <#> --add-assignee @me`.
- **Resolve a ticket:** post the answer as a comment (`gh issue comment`), close it
  (`gh issue close <#> --comment "..."`), and append a one-line gist + link to the
  map's **Decisions so far** section (`gh issue edit <map#> --body ...`).
- **Labels used by wayfinder** (create on first use with `gh label create`):
  `wayfinder:map`, `wayfinder:research`, `wayfinder:prototype`,
  `wayfinder:grilling`, `wayfinder:task`.
