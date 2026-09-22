---
name: goosar-creating-agents
description: "Use when creating, inspecting, or debugging a Goosar agent through the `goosar agent` CLI or `POST /api/agents` — what each field is, its persisted shape, whether it is metadata-only or consumed by the daemon at claim time, which inputs are validated/rejected, how custom_env secrets are gated, and how skill binding behaves. Not for assigning issues to existing agents or for runtime task prompts."
user-invocable: false
allowed-tools: Bash(goosar *)
---

# Creating Goosar agents

This is the contract for Goosar's agent-creation path: what the create entry
points accept, what the server validates and rejects, how each field is
persisted, and which fields the daemon actually reads at claim time. It is
not a parameter manual — it states source-traced facts, and every claim is
backed by `file:line` in `references/creating-agents-source-map.md`.

## Quick start (read-only inspection)

These commands read state and have no side effects:

```bash
goosar agent get <agent-id> --output json      # full persisted agent record
goosar agent skills list <agent-id> --output json   # current skill bindings
goosar agent env get <agent-id> --output json  # env keys; values in plaintext only for the agent's owner, masked otherwise, agents denied
```

`agent get` returns the persisted agent including `runtime_id`, `model`,
`thinking_level`, `service_tier`, `custom_args`, `has_custom_env`,
`custom_env_key_count`, and `skills`. It never returns plaintext `custom_env`.

## Core model

An agent is a workspace-scoped row (table `agent`). Creation is a single
`POST /api/agents` (`goosar agent create`). At task claim time the daemon
re-reads the agent row and assembles the runtime payload — so the persisted
fields, not the create-time output, are what the agent runs on.

Two distinct text fields, often confused:

- `description` is a catalog summary. It is stored and shown in listings; the
  daemon does NOT inject it into the agent's runtime prompt. Treat it as
  human-facing metadata only. Capped at 255 Unicode code points.
- `instructions` is the runtime behavior contract. The daemon reads it at
  claim time and ships it to the provider as the agent's durable instructions.
  Persona, responsibilities, boundaries, output and escalation rules go here,
  not in `description`.

## CLI / API entry points

Minimum create call (`--name` and `--runtime-id` are both required):

```bash
goosar agent create --name <name> --runtime-id <runtime-id> \
  --description "<short catalog summary>" \
  --instructions "<runtime behavior contract>" \
  --output json
```

`runAgentCreate` builds a JSON body and posts it to `/api/agents`. It only
adds a key when its flag was provided — `description`/`instructions` on a
non-empty value, the rest (`runtime-config`, `custom-args`, `model`,
`thinking-level`, `service-tier`, `permission-mode`, `visibility` (legacy,
mapped to permission mode), …) on the flag being `Changed` — so omitted
flags fall through to server defaults rather than sending empty strings.

The HTTP body (`CreateAgentRequest`) accepts: `name`, `description`,
`instructions`, `avatar_url`, `runtime_id`, `runtime_config`, `custom_env`,
`custom_args`, `model`, `thinking_level`, `service_tier`, `permission_mode`,
`invocation_targets`, `visibility` (legacy), `max_concurrent_tasks`,
`mcp_config`, `composio_toolkit_allowlist`, `template`, `skill_ids`.
`permission_mode` (MUL-3963) is authoritative when present: `private` (owner
only) or `public_to` (allow-list in `invocation_targets`); when absent, legacy
`visibility` is mapped (`private` → private, `workspace` → public_to +
workspace target). The response exposes `permission_mode` and keeps
`visibility` only as a derived legacy field.

## Copying an agent

`goosar agent copy <source-agent-id>` forks an existing agent's portable
configuration into a brand-new agent, leaving the source untouched. It is the
CLI/headless equivalent of the web "Duplicate" action. No dedicated server API
is involved: `runAgentCopy` reads the source with `GET /api/agents/<id>`, then
POSTs a `CreateAgentRequest` — passing the source's skill ids in `skill_ids` so
the bindings attach in the SAME create transaction (unlike `agent create`, which
binds nothing). The mutation is therefore a single atomic create.

```bash
goosar agent copy <source-agent-id> --name "My Agent (copy)"   # same runtime
goosar agent copy <source-agent-id> --runtime-id <target> --model <model>  # cross-runtime fork
```

- Copied by default, each overridable with the matching flag: `name` (suffixed
  `" (copy)"`), `description`, `instructions`, avatar, `custom_args`,
  `max_concurrent_tasks`, invocation permission (`permission_mode` +
  allow-list), and assigned workspace skills.
- Runtime-specific fields (`model`, `thinking_level`, `service_tier`) are copied
  ONLY when the target runtime is unchanged. `--runtime-id` selecting a
  different runtime drops them and REQUIRES `--model` (pass `--model ""` to
  accept the target runtime default), mirroring the web Duplicate clearing model
  on a runtime switch.
- Never copied: `custom_env`, `mcp_config`, `runtime_config` (secret /
  machine-local; redacted or masked on read anyway). Supply fresh values with
  the same secret-safe flags as `agent create` (`--custom-env*`, `--mcp-config*`,
  `--runtime-config`), or with `agent env set` after the copy exists.
- `--no-skills` skips copying the source's skill bindings.

## Field contracts

| Field | Persisted as | Validated? | Consumed by |
|---|---|---|---|
| `name` | `agent.name` | required, 400 if empty | listings, runtime payload |
| `description` | `agent.description` | 400 if > 255 code points | catalog/listing only — NOT the runtime prompt |
| `instructions` | `agent.instructions` | none | daemon → provider at claim time |
| `avatar_url` | `agent.avatar_url` | none; an explicit non-empty value is preserved, while omitted/empty creates a random `emoji:<glyph>` avatar | catalog/listing UI only — NOT the runtime prompt |
| `runtime_id` | `agent.runtime_id` | required (400) + must resolve to a runtime in this workspace | selects runtime/provider |
| `model` | `agent.model` (nullable) | none beyond runtime support | daemon reads; empty = runtime default |
| `thinking_level` | `agent.thinking_level` (nullable) | provider-level enum; unknown literal → 400 | daemon; empty = runtime default |
| `service_tier` | `agent.service_tier` (nullable) | Codex-only safe token; other providers reject; exact model/tier pair checked by daemon | daemon → Codex app-server; empty = local Codex config |
| `custom_args` | `agent.custom_args` (JSON array) | JSON shape checked CLI-side; server stores as-is | daemon (extra CLI switches); defaults to `[]` |
| `runtime_config` | `agent.runtime_config` (JSON) | JSON shape checked CLI-side; server stores as-is | runtime-specific config; defaults to `{}` |
| `custom_env` | `agent.custom_env` (JSON object) | — | daemon (process env); see Env & secrets |
| `mcp_config` | `agent.mcp_config` (raw JSON) | CLI checks it is a JSON object or `null`; server encrypts it at rest when `GOOSAR_MCP_SECRET_KEY` is set (plaintext with a startup warning otherwise). At create, literal `null` is dropped (no-op); at update, `null` clears the column | daemon → provider (provider-specific MCP handling); redacted on read |
| `permission_mode` + `invocation_targets` | `agent.permission_mode`, invocation-target rows | `private` or `public_to`; targets required only for `public_to` | access control (MUL-3963); authoritative over `visibility`; gates who can read/route a private agent (e.g. a private squad leader) — NOT the runtime prompt |
| `visibility` | `agent.visibility` | legacy; ignored when `permission_mode` is present, otherwise mapped to it | derived legacy field on reads; defaults to `private` |
| `max_concurrent_tasks` | `agent.max_concurrent_tasks` | — | scheduler task cap; defaults to `6` |

Defaults when omitted: `runtime_config` → `{}`, `custom_env` → `{}`,
`custom_args` → `[]`, `avatar_url` → a random `emoji:<glyph>`, `visibility` →
`private`, `max_concurrent_tasks` → `6`
(all materialized server-side before the insert). `custom_args`/`runtime_config`
are typed `[]string`/`any` and marshaled as-is — the JSON-shape rejection
happens in the CLI, not the create handler.

`thinking_level` is validated only at the provider level: fixed-catalog
providers reject an unrecognized literal, while dynamic-catalog providers such
as Codex/OpenCode accept a syntactically safe token. A value unsupported for
the chosen model is NOT rejected here — the daemon checks its local model
catalog at execution time, logs a warning, and omits the incompatible override.

Set it from the CLI with `--thinking-level` on `agent create` and `agent
update`, mirroring `--model`: the flag is a thin pass-through to the top-level
`thinking_level` field, and on update an empty string (`--thinking-level ""`)
clears it back to the runtime default. The CLI deliberately does not enumerate
the valid levels — they are runtime/model-specific (Claude currently uses
`low|medium|high|xhigh|max`; Codex values are discovered from the runtime's
model catalog). It forwards the token, the server applies the provider's
fixed-enum or safe-token gate, and the daemon performs the exact model/level
check. A runtime whose provider has no thinking concept rejects any non-empty
value with a 400.

`service_tier` is the matching first-class Codex speed control. Set it with
`--service-tier <catalog-id>` on create/update; use `--service-tier ""` on
update to clear it. The runtime model catalog owns both availability and
display copy (currently `priority`, shown as Fast). The server accepts safe
future Codex catalog IDs, while the daemon verifies the exact model/tier pair
before execution and omits a stale incompatible override. Agents without an
explicit model fail closed because the effective config.toml model is unknown.

### model vs custom_args

`model` is a first-class persisted column the daemon reads directly.
`custom_args` are raw provider CLI args. The CLI help notes that some providers
(codex app-server, openclaw) reject `--model` inside `custom_args` — but that is
documented CLI guidance, not a server-enforced invariant; nothing in the create
handler inspects `custom_args` for a model flag.

Never put credentials or other secrets in `custom_args`. Daemon command logs
redact argument values, but the values still live in the provider process's
argv and may be visible to other local processes through `ps` or `/proc`. Put
provider credentials in `custom_env` instead.

## Env & secrets

`custom_env` is secret material. The CLI offers three input channels; two keep
secrets out of shell history and the process list:

```bash
goosar agent create --name <name> --runtime-id <runtime-id> --custom-env-stdin --output json
goosar agent create --name <name> --runtime-id <runtime-id> --custom-env-file <0600-json> --output json
```

`--custom-env-stdin` reads the JSON object from stdin; `--custom-env-file`
reads it from a file (suggested mode 0600). The third channel,
`--custom-env <json>`, puts the value on the command line where shell history
and `ps` can see it — avoid it for real secrets.

Read-side facts (these are the wrong assumptions to avoid):

- Agent resources never expose plaintext `custom_env`. `agent
  list/get/create/update` and WS events return only `has_custom_env` (bool) and
  `custom_env_key_count` (int).
- Values are read through the dedicated `GET /api/agents/{id}/env` endpoint
  (`goosar agent env get`), and **only the agent's own owner gets plaintext**.
  A workspace owner/admin reaching another member's agent is answered with the
  same key names and `****` in place of every value, plus `values_masked: true`
  (GH #265). **Agent actors are denied** outright regardless of the backing
  member's role — a running agent cannot read another agent's secrets. The
  audit row differs accordingly: `agent_env_revealed` (with `revealed_keys`)
  for a real reveal, `agent_env_listed` for a masked listing.
- The endpoint is reachable by the agent's owner whatever their workspace role
  is, and by workspace owner/admin for any agent. Those two together are the
  gate; a plain member cannot reach somebody else's agent env at all.
- Writing values after creation does NOT go through `agent update`. The generic
  update handler rejects any `custom_env` field with a 400 ("use PUT
  /api/agents/{id}/env"). Plaintext env writes are handled by
  `PUT /api/agents/{id}/env` (`goosar agent env set`), which writes an audit
  row. Its response follows the same rule as the read: a non-owner is answered
  with masked values, so a save cannot be used to read back what the GET hid.
- The write path never requires reading first. `****` as a value means "keep
  whatever is stored under this key", so a non-owner can add a key, replace a
  key, or drop a key while leaving untouched entries exactly as the owner set
  them. The flip side: the map is replaced wholesale, so any key absent from
  the request is removed. Two payloads are rejected with 400 rather than
  guessed at: `****` under a key that is not stored (nothing to keep), and a
  value that merely starts with `****` (what an input pre-filled with the
  marker produces when a new secret is typed onto the end of it).

### mcp_config

`mcp_config` is the agent's OWN MCP server configuration (a JSON object such as
`{"mcpServers": {…}}`). It is no longer the only source of an agent's MCP
servers: the workspace also has a shared MCP library that is assigned to agents
one at a time — see "Workspace MCP servers" below. At claim time the two are
merged, and the agent's own entry wins on a name collision.

It is also secret material — MCP entries routinely embed API tokens — and offers
the same three input channels as `custom_env`, on BOTH `agent create` and
`agent update`:

```bash
goosar agent create --name <name> --runtime-id <runtime-id> --mcp-config-file <0600-json> --output json
goosar agent update <agent-id> --mcp-config-stdin --output json
goosar agent update <agent-id> --mcp-config 'null'   # clears the config
```

`--mcp-config-stdin` / `--mcp-config-file` keep the value out of shell history
and `ps`; the inline `--mcp-config <json>` does not. The CLI requires a JSON
**object** or the literal `null`; a top-level array or primitive is rejected
client-side, and empty stdin/file input errors rather than silently clearing.

Two ways `mcp_config` differs from `custom_env`:

- **It IS settable through `agent update`.** Unlike `custom_env`, `mcp_config`
  has no dedicated audited endpoint — the generic `PUT /api/agents/{id}` accepts
  it. Tri-state per the raw request body: field omitted → no change; `null` →
  clear; object → replace.
- **It is serialized on read, but masked.** `agent get`/`list` return the
  stored document only to the agent's own owner. Everyone else — workspace
  owner/admin included, since GH #265 — gets the same containers and the same
  server NAMES with each server's configuration replaced by
  `{"__goosar_masked__": true}`, plus `mcp_config_redacted: true`. The
  mutation responses of `agent create`/`update` and archive/restore follow the
  same rule, so a no-op update is not a way around the read gate.
- **The placeholder round-trips, exactly like `****` does for env.** Send a
  server back as `{"__goosar_masked__": true}` and the server substitutes the
  stored configuration for that name; omit the name and the server is removed.
  So a non-owner can revoke one leaked entry without holding the others.
  Sending a REAL object is the agent OWNER's move alone: for anyone writing
  through the masked view it is a 403, because an MCP server entry is a
  command the daemon spawns on the owner's machine with the owner's
  environment in scope. A placeholder under a server name that is not stored
  is a 400 — it cannot be honoured, and neither storing nor dropping it
  silently would be right. On `agent create` a placeholder is a 400 too: there
  is nothing to keep.
- **An agent with no owner has no owner path.** `owner_id` is nullable, and
  for such a row every caller writes through the masked view — so its MCP
  entries can be removed and its document cleared, but a new entry cannot be
  authored by anybody until the row has an owner. Reading its values is
  likewise closed to everyone, deliberately: otherwise "remove the member,
  then read what their agent still carries" would be a supported move.
- **Two cases still answer with `mcp_config: null` plus the flag**: an agent
  actor (a running agent must not even inventory a sibling's MCP servers) and
  a workspace with `always_redact_env` set, which strips the field for
  everyone including the agent's owner. Responses also carry
  `mcp_config_encrypted`: `true` when the stored value is encrypted at rest
  (`GOOSAR_MCP_SECRET_KEY` set), `false` when it sits in the database as
  plaintext.

Self-hosted deployments may enforce a server-side MCP allowlist policy
(`GOOSAR_MCP_ALLOWED_HOSTS` for referenced hostnames — remote `url` values
plus any `http(s)://` URL inside `args` or `env` values, loopback exempt;
`GOOSAR_MCP_ALLOWED_COMMANDS` for `command` entries, which must then be bare
executable names matched by basename). Both stored shapes are policed —
`mcpServers` and the OpenCode-native top-level `mcp` map. A create or update
whose `mcp_config` references anything outside the allowlists fails with 400
naming the variable; on the perimeter delivery profile the unset default is
the corporate preset surface, so non-preset entries need the operator to
extend the list. The same deployment may restrict agent providers
(`GOOSAR_ALLOWED_PROVIDERS`): creating an agent on — or moving it to — a
runtime whose provider is outside the list fails with 403, and `/api/config`
advertises the effective list as `allowed_providers` (omitted when
unrestricted).

Provider support is not uniform: Qwen Code accepts a managed `mcp_config` through a daemon-owned 0600 temporary JSON file passed with `--mcp-config`; it is removed when the run exits. Leave the field unset (`null`) to inherit Qwen Code native settings.

An individual server entry may carry `"enabled": false` (or `"disabled": true`)
to keep it stored-but-inactive — the Capabilities → MCP tab shows a "Disabled"
badge, and every provider dispatch path filters the entry out before launch:
ACP providers skip it when building `session/new`, Claude / CodeBuddy / Qwen
Code drop it from the temp `--mcp-config` file, Codex drops it from the
managed `[mcp_servers.*]` TOML block, and the daemon's runtime-local merge
excludes disabled entries on both the runtime and agent sides (a disabled
agent entry also suppresses a same-name runtime-local server). Malformed flag
values fail open — the entry stays active — with a warning in the daemon log.
Onboarding-seeded corporate presets use this: they ship disabled with empty
credential slots until the user fills them in and removes the flag.

## Workspace MCP servers

A workspace also keeps a LIBRARY of shared MCP servers. It is shaped like
workspace skills, and the shape is the point: adding a server to the library
gives it to NOBODY. It reaches an agent only when someone assigns it to that
agent, and the assignment carries its own on/off toggle.

```bash
goosar workspace mcp list
goosar workspace mcp add <name> -            # configuration on stdin
goosar workspace mcp update <server-id> - --name <new-name>
goosar workspace mcp remove <server-id>

goosar agent mcp list <agent-id>
goosar agent mcp add <agent-id> <server-id>
goosar agent mcp disable <agent-id> <server-id>
goosar agent mcp enable <agent-id> <server-id>
goosar agent mcp remove <agent-id> <server-id>
```

What to know before using it:

- **The stored configuration is write-only for everyone.** No endpoint returns
  a shared server's `url`, `command`, `args`, `headers`, or `env` — not even to
  the member who typed it in. Reads carry the name and the transport only, so
  `update` REPLACES an entry: supply the whole configuration again. This is
  stricter than the masked view `mcp_config` gets, because a shared entry has
  no single owner whose secret it is.
- **Write-only ends at assignment — this is an accepted risk, not a gap.** At
  claim time an assigned, enabled server's DECRYPTED configuration is written
  into the task-local config on the machine the agent claims from. From there
  any member who can give the agent tasks can read it transitively (for
  example, by asking the agent to print its own config file). An MCP server
  with embedded credentials cannot work any other way — the runtime needs the
  plaintext. Assign a shared server only to agents whose task audience you
  trust with that credential; the assignment dialog says the same.
- **The entry must not carry `enabled` / `disabled`.** The on/off switch lives
  on the agent↔server assignment. A toggle inside the entry would also switch
  off a same-named server defined locally on every machine the agent runs on
  (the daemon treats a disabled name as off, full stop), so the write is
  rejected with 400.
- **Assignments are read on every claim.** Assigning a server or flipping its
  toggle applies to the agent's next task; nothing has to be recreated.
- **Permissions.** The library is written by workspace owner/admin and readable
  by any member (it carries no credential material). Assignments are ALSO
  owner/admin-only — not the agent's owner: assigning a server routes its
  decrypted configuration to whatever machine the agent claims from, so letting
  any member with an agent assign one would let them read any shared secret.
  Every write refuses an agent actor.
- **Assignment also runs code on the agent owner's machine — the reverse
  direction is an open, documented risk.** A workspace admin who is not the
  agent's owner can author a library entry with any transport (including a
  stdio `command`) and assign it; the owner's daemon launches it on the next
  claim. The deployment MCP allowlist (`FilterConfig`, #65) still applies at
  claim time, but where no allowlist is configured this is admin→owner code
  execution by design. The GH #273 owner-only rule and this assignment gate
  pull in opposite directions; a dual-consent flow (admin shares, owner
  attaches) is the known fix and is a pending product decision. The same
  tension applies to skills: workspace owner/admin can attach any workspace
  skill to another member's agent, and skill content steers the owner's
  runtime just as instructions do.
- **Storage.** The configuration is sealed at rest with `GOOSAR_MCP_SECRET_KEY`
  and is fail-closed: with no key configured, creating or updating a library
  entry answers 503 rather than storing plaintext.
- **This is not `workspace-config`'s `mcp_defaults`.** That layer carries only
  `enabled` and `env` for a name a machine already defines by hand; it cannot
  create a server. The library supplies the DEFINITION and the addressing, the
  config layers supply values and machine policy. They meet by name inside the
  daemon, and a library entry with the same name REPLACES the machine entry
  wholesale — env included — which is why a library entry carries its own env.

## Skill binding

Creating an agent through the CLI's `goosar agent create` does NOT bind any
workspace skill — binding is a separate call after the agent exists. (The HTTP
API itself accepts `skill_ids` on create and binds them inside the same
transaction as the agent row; the CLI simply does not expose that field on
`create` — `goosar agent copy` uses it to carry the source's bindings over.) Two distinct verbs:

- `add` is additive — it merges the given ids with existing bindings
  (`POST /api/agents/{id}/skills/add`).
- `set` is replace-all — it overwrites the entire binding list with exactly
  the given ids (`PUT /api/agents/{id}/skills`); `--skill-ids ''` clears all.

```bash
goosar agent skills add <agent-id> --skill-ids <skill-id> --output json
goosar agent skills list <agent-id> --output json
```

At claim time the daemon assembles the agent's skills as workspace-bound skills
FIRST, then appends the platform built-in skills. `LoadAgentSkills` loads each
bound skill's content plus its supporting files; built-in skills are embedded
at compile time and loaded from `SKILL.md` + sibling files. Both reach the
provider as skill content — which is why capability belongs in a bound skill,
not pasted into `instructions`.

## Side effects needing approval

Read-only (safe): `agent get`, `agent skills list`, `agent env get`.

State-changing (require an explicit instruction — do not run speculatively):

- `goosar agent create` — inserts a new agent row.
- `goosar agent copy` — inserts a new agent row (a fork of an existing agent);
  the source is left untouched.
- `goosar agent skills add` / `set` — mutate bindings (`set` is destructive:
  it drops bindings not in the new list).
- `goosar agent env set` — overwrites the full `custom_env` map and writes an
  audit row.

## Common wrong assumptions

- "`description` is the prompt." It is not — only `instructions` reaches the
  runtime. A rich description with empty instructions yields a named shell with
  no operating contract.
- "Create binds the agent's skills." It does not; bind explicitly afterward.
- "`agent update` can rotate env." It cannot — it 400s on `custom_env`; use the
  env endpoint.
- "`mcp_config` behaves like `custom_env` on update." It does not — `mcp_config`
  IS settable via `agent update` (`--mcp-config`), with `--mcp-config null` to
  clear; only `custom_env` is gated behind the dedicated env endpoint.
- "`agent get` shows env values." It shows only `has_custom_env` and
  `custom_env_key_count`.
- "Workspace owner/admin can read any agent's secrets." Not through the API
  since GH #265. Values come back masked; plaintext goes to the agent's owner
  only. What they may still WRITE is narrow and uniform since GH #273:
  `custom_env` keys and `mcp_config` entries they may only remove or preserve
  (adding or replacing a value is owner-only, 403), and `runtime_config`,
  `instructions` and `custom_args` they cannot change at all. Do not describe
  any of it as "manage — add, replace, remove".
- "So a stored value never leaves the agent's owner." One copy does: the
  machine hosting the runtime the agent runs on receives `custom_env` and the
  decrypted `mcp_config` in the claim payload, because the process it starts
  needs them. That is why moving an agent to a different runtime is an
  owner-only write (GH #218): a non-owner rebind is a 403 regardless of the
  destination; only an unchanged `runtime_id` echo is tolerated. If you are
  choosing a runtime for your agent, you are choosing who holds its
  credentials.
- "The `****` I read back from `env get` is the value." It is a placeholder
  for a value you were not shown (`values_masked: true`). Never write it into
  another system as if it were a credential, and never treat two `****`
  entries as equal values. `{"__goosar_masked__": true}` is the same thing
  for one MCP server.
- "`always_redact_env` protects `custom_env`." It never has. The workspace
  setting only strips `mcp_config` — from everyone, the agent's owner
  included, which also removes the managed-MCP surface. Env is governed by the
  owner rule above regardless of it, and that rule needs no setting.
- "An invalid `thinking_level`/`model` combo is caught at create." Only an
  unknown provider-level literal is — model-specific gaps fail at run time.
- "`set` and `add` are interchangeable for skills." `set` replaces all
  bindings; using it when you meant `add` silently removes capabilities.

## References

`references/creating-agents-source-map.md` maps every contract above to its
`file:line` on the current tree, the runtime effect, and a safe read-only
verification command.
