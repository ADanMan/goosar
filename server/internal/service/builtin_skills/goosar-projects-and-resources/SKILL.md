---
name: goosar-projects-and-resources
description: "Use when creating, inspecting, updating, or debugging Goosar projects and project resources. Covers durable project context, github_repo and local_directory resources, how resources affect future agent task context, when to bind repos, and when not to mutate resources."
user-invocable: false
allowed-tools: Bash(goosar *)
---

# Goosar Projects and Resources

## Quick start

Projects are durable context containers. Resources attached to a project can affect future agent tasks.

```bash
goosar project list --output json
goosar project get <project-id> --output json
goosar project resource list <project-id> --output json
```

Project resources are mutated through project resource commands/endpoints. Issue
comments do not create durable project resources.

## Core model

A project groups work and carries durable resources. A resource is not just display metadata; it is context later injected into task briefs and `.goosar/project/resources.json`.

A project's `description` is also durable context: when an issue (or a quick-create task) is bound to a project, the project description is injected into the agent's brief under `## Project Context` and written to `.goosar/project/resources.json` as `project_description`. Use it for project-wide rules/context that should apply to every task in the project.

Common resource types:

- `github_repo` — durable GitHub repo context, with `resource_ref.url`, optional checkout `ref`, and optional prompt-only `default_branch_hint`;
- `local_directory` — daemon-local path context, with `resource_ref.local_path`, `daemon_id`, and optional label.

## CLI

```bash
goosar project list --output json
goosar project get <project-id> --output json
goosar project create --title "<title>" --repo <github-url> --output json
goosar project create --title "<title>" --start-date 2026-03-01 --due-date 2026-03-31 --output json
goosar project update <project-id> --title "<title>" --output json
goosar project update <project-id> --due-date 2026-04-15 --output json
goosar project update <project-id> --start-date "" --output json   # clear the start date
goosar project status <project-id> in_progress --output json
goosar project resource list <project-id> --output json
goosar project resource add <project-id> --type github_repo --url <github-url> --output json
goosar project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json
goosar project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --output json
goosar project resource update <project-id> <resource-id> --url <new-github-url> --output json
goosar project resource update <project-id> <resource-id> --ref <branch-or-sha> --output json
goosar project resource remove <project-id> <resource-id> --output json
```

For `github_repo`, non-JSON `--ref` sets `resource_ref.ref`, the default checkout branch/tag/SHA for future tasks in that project. JSON `--ref '<json>'` remains the escape hatch for full payloads or resource types not covered by shortcuts.

`--start-date` / `--due-date` are optional calendar days (`YYYY-MM-DD`, like issue dates). On `project update`, pass an empty string (`--start-date ""`) to clear a date; an unset flag leaves it untouched.

## When to add a resource

Add/update a project resource when the user asks for durable project context: "把这个 GitHub repo 绑到项目上", "以后都用这个 repo", "agent 总是拿不到这个项目的仓库", or "这个项目要在我的本地目录里跑".

Project resources are durable and affect future tasks. `goosar repo checkout`
is task-local checkout state.

## Debugging wrong context

1. `goosar project get <project-id> --output json`.
2. `goosar project resource list <project-id> --output json`.
3. Check `github_repo.resource_ref.url`, optional `ref`, `default_branch_hint`, and `local_directory.resource_ref.daemon_id`.
4. Updating resources is a durable mutation. After an update, listing the
   resource is the verification path.
5. If resources match the expected task context, inspect runtime/repo checkout
   path next.

## Side effects

Project create/update/delete/status and project resource add/update/remove mutate durable workspace state and affect future tasks. Ask before changing `local_directory` unless the user explicitly requested that exact local path.

More source-backed details: `references/projects-and-resources-source-map.md`.
