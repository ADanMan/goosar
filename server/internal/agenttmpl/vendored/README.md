# Vendored agent-template skills

Offline snapshot of every upstream skill referenced by the built-in agent
templates (`../templates/*.json`), embedded into the server binary via
`../vendored.go` (issue #66). Creating an agent from a built-in template
materialises skills from this snapshot with **zero network access**; the
live GitHub fetch in `handler/agent_template.go` is only a fallback for a
template skill ref without a vendored copy, and only when the `github`
source is enabled via `GOOSAR_SKILL_SOURCES`.

Layout:

- `manifest.json` — maps each template `source_url` to its snapshot
  directory, with upstream provenance (`owner`/`repo`/`ref`/`path`) and the
  exact `commit` the snapshot was taken from.
- `skills/<dir>/` — the skill content: `SKILL.md` plus supporting files,
  filtered exactly like the live importer (`handler/skill.go`): binary
  assets (fonts, images, archives, …) and `LICENSE*` files are dropped.

## Refreshing the snapshot

Do not edit these files by hand. To pull the latest upstream content, or
after adding/changing a `source_url` in any template:

```bash
scripts/sync-vendored-template-skills.sh
(cd server && go test ./internal/agenttmpl/... -count=1)
git diff --stat server/internal/agenttmpl/vendored
```

Review the diff (this is third-party content that agents will execute as
instructions — read what changed, not just the stats), then commit the
regenerated `vendored/` tree together with any template change in the same
PR. `TestVendoredCoversEveryTemplateSkill` fails CI when a template
references a skill that has no vendored copy.
