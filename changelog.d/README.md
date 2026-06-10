# Changelog fragments

Add a changelog entry by creating a **new file** in the section subdirectory
that fits your change:

| Directory                       | Section in the released changelog        |
| ------------------------------- | ---------------------------------------- |
| `changelog.d/notable-changes/`  | Notable Changes (major, release-defining) |
| `changelog.d/cli/`              | CLI                                      |
| `changelog.d/bundles/`          | Bundles                                  |
| `changelog.d/dependency-updates/` | Dependency updates                     |
| `changelog.d/api-changes/`      | API Changes                              |

Because every PR adds its own file, two PRs never touch the same path and so
they never conflict — unlike editing the shared `NEXT_CHANGELOG.md` directly.

## How to add an entry

Name the file after your PR number so the entry links back to it — either bare
(`changelog.d/cli/5464.md`) or with a description (`changelog.d/cli/5464-quickstart.md`).
Write the entry as the file's contents:

```
Added the `databricks quickstart` command.
```

- **No PR link needed** — the `(#5464)` reference is added automatically from
  the filename and expanded to a full link (see `tools/update_github_links.py`).
  Write your own `(#NNNN)` only to point at a different or additional PR.
- The leading `* ` is optional — it's added for you when entries are collated.
- One file is usually one entry. For several entries, put each on its own
  `* ` line.

See [`.agent/skills/pr-checklist/SKILL.md`](../.agent/skills/pr-checklist/SKILL.md)
for when an entry is warranted.

## How it's released

You don't run anything. At release time, `tools/collate_changelog.py` folds
every fragment into the matching section of `NEXT_CHANGELOG.md` and deletes the
fragments; the release tooling then generates `CHANGELOG.md` from
`NEXT_CHANGELOG.md` as before. `./task changelog-check` validates fragment
placement on every PR.
