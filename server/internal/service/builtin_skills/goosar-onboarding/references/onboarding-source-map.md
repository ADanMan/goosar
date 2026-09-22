# Onboarding Source Map

This file records source evidence for `goosar-onboarding/SKILL.md`.

Use this when the task requires exact source paths, edge-case behavior, or
contract verification. Every claim the skill makes about product behavior is
listed here with the code that makes it true; a claim with no entry below does
not belong in the skill.

## The greeting is already spent

Source:

```text
packages/views/workspace/welcome-after-onboarding.tsx
packages/views/locales/ru/onboarding.json   # welcome_after_onboarding.runtime.greeting
```

Key facts:

- The post-onboarding screen greets the person by name before the chat opens,
  which is why the skill's first rule is "continue, do not start over".

## The first task actually starts

Source:

```text
server/internal/handler/issue.go   # CreateIssue: status defaults to "todo"
server/internal/handler/issue.go   # shouldEnqueueAgentTask
server/cmd/goosar/cmd_issue.go    # issue create flags
```

Key facts:

- `CreateIssue` defaults an omitted status to `todo` (`status = "todo"` when
  `req.Status == ""`).
- `shouldEnqueueAgentTask` returns false for `status == "backlog"` and
  otherwise defers to `isAgentAssigneeReady`. So a `todo` issue assigned to a
  READY agent enqueues that agent on creation, and a `backlog` issue does not
  — backlog is a parking lot, and moving out of it is handled separately in
  `UpdateIssue`.
- "Ready" is not the same as "assigned": `isAgentAssigneeReady` requires
  `assignee_type == "agent"`, a loadable agent row, `agent.runtime_id` set,
  and `archived_at` unset. An agent bound to no runtime enqueues nothing and
  says nothing — which is why the skill sends the agent to `goosar status`
  (`caller.runtime_status`) before calling a task started. The check is
  runtime-BOUND, not runtime-online: a bound but offline runtime still
  enqueues, and the task waits for the daemon to come back.
- `goosar issue create` accepts `--title`, `--description-file`,
  `--assignee-id`, `--status`, `--output`. `--description` decodes `\n`,
  `\r`, `\t` and `\\`, which is why the skill teaches `--description-file`
  (read verbatim; the path must sit inside the working directory unless
  `--allow-external-file` is set).
- Valid statuses are `backlog, todo, in_progress, in_review, done, blocked,
  cancelled` (`validIssueStatuses`).

## The role page to hand off to

Source:

```text
packages/views/capabilities/capabilities-page.tsx
packages/core/paths/paths.ts                      # capabilities: `${ws}/capabilities`
apps/web/app/[workspaceSlug]/(dashboard)/capabilities/page.tsx
server/cmd/server/router.go                       # GET /capabilities → h.GetWorkspaceCapabilities
packages/views/locales/ru/workspace.json          # capabilities.title / lede
```

Key facts:

- The page is workspace-scoped at `/<workspace-slug>/capabilities`.
- Its content comes from the workspace role template
  (`workspace_template.capabilities`), four role-independent baseline lines in
  i18n, and the resolved service credentials — not from a list written in the
  page.
- The page's own tone rules already forbid "perimeter", "runtime",
  "provisioning" and package names. The skill's "what not to say" rule is the
  same rule applied to the conversation, so the two surfaces do not contradict
  each other.
- The page exists precisely because the post-onboarding welcome modal was
  one-shot, needed a live runtime and quota, and answered in product
  vocabulary. That is why the skill hands off to it instead of listing
  features in chat.

## The role cannot be looked up from the CLI

Source:

```text
server/cmd/server/router.go            # GET /api/workspaces/{id}/capabilities
server/cmd/goosar/cmd_workspace.go    # workspace subcommands: list, create, get, member, invite, update, switch
```

Key facts:

- `GetWorkspaceCapabilities` is an HTTP endpoint with no `goosar` CLI verb
  in front of it. There is no `goosar workspace capabilities`.
- `template_key` is optional in the create form
  (`packages/views/workspace/create-workspace-form.tsx` sends it only when the
  picker is non-empty), so a workspace usually carries no role at all and the
  page falls back to its four baseline lines.
- Both facts are why the skill tells the agent to ask the person about their
  work rather than to look the role up.

## The workspace slug

Source:

```text
server/cmd/goosar/cmd_workspace.go    # workspace get [workspace-id|slug|prefix]
```

Key facts:

- `goosar workspace get --output json` returns the workspace record including
  its `slug`, which is the first path segment of the capabilities link.

## System state, read but never spoken

Source:

```text
server/internal/handler/goosar_status.go   # GetGoosarStatus
server/cmd/goosar/cmd_status.go            # goosar status
server/cmd/server/router.go                 # GET /api/status (workspace-scoped, not behind RequireHumanActor)
```

Key facts:

- `goosar status --output json` reports runtimes, provisioning, MCP,
  perimeter and LLM state from signals that already exist. It is available to
  an agent actor on purpose (issue #286).
- Every section reports `unknown` when the server has no signal, and never
  reports green by default: Kerberos is always `unknown` (the server never
  sees a machine-local ticket cache), and MCP `tools_verified` is always
  `unknown` (nothing records whether a declared server actually served tools).
- The response carries no credential values — the LLM block reports
  `has_api_key`, not the key. That is why the skill can tell an agent to read
  it while forbidding it to ask a person for keys.
- Scope follows the actor: an agent caller gets no `mcp.workspace_servers`
  count (`/api/workspace-mcp-servers` is human-only so the shared library's
  inventory is not published to agents), only the servers assigned to itself.
  A member caller gets the count.
- `provisioning.state` is `ok` only when a delivery row matches a currently
  ENABLED pin. The delivery table is cumulative "ever served" memory, so
  counting it raw would report `ok` for a workspace that repinned yesterday
  and whose machines have not fetched since.

## Turn boundaries

Source:

```text
server/internal/service/builtin_skills/goosar-working-on-issues/SKILL.md
```

Key facts:

- An agent's turn ends when it posts its reply; there is no background
  continuation in which it returns with a result. The "never promise to come
  back" rule is that boundary stated in first-session terms.

## Deliberately absent from the skill

- **Starter cards under the greeting** (issue #270, point 2). Fixed one-tap
  first messages must be rendered by the chat UI to exist; text in a skill
  cannot put a button on screen. Nothing here claims they exist.
- **Role templates** (#269). When role templates give a board worth
  proposing, this skill's "one intention, one task" default is the place to
  extend — not a new section listing roles.
- **Personal credential entry** (#251). The skill forbids asking for keys in
  chat and points at the product step instead.
