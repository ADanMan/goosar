# Creating agents — source map

Evidence layer for `SKILL.md`. Every contract maps to `file:line` on the
current tree, the runtime effect, and a safe read-only check. Line numbers were
re-derived against this tree — re-derive again if the files move, the
surrounding context (not the number) is the anchor.

## Verification

```bash
# Conformance eval for this skill (and the shared template invariants):
go test ./internal/service -run TestCreatingAgentsSkillCoversAgentCreationContracts
go test ./internal/service -run TestBuiltinSkillsConformToTemplate
```

## CLI entry points — `server/cmd/goosar/cmd_agent.go`

| Contract | Line | Behavior | Safe check |
|---|---|---|---|
| Create flags: `name`, `description`, `instructions`, `runtime-id` | 160–163 | Registered create flags; `name`/`runtime-id` enforced in `runAgentCreate` | `goosar agent create --help` |
| `runtime-config`, `model`, `thinking-level`, `service-tier`, `custom-args` flags | 164–168 | `model` help: "Prefer this over passing --model in --custom-args"; thinking and Codex service-tier values are thin catalog-owned pass-throughs, with exact model compatibility checked by the daemon; empty = runtime default | `goosar agent create --help` |
| Secret-safe env input: `custom-env`, `custom-env-stdin`, `custom-env-file` | 169–171 | `--custom-env` warns about shell history / `ps`; stdin and file modes keep secrets off the command line; mutually exclusive | `goosar agent create --help` |
| Secret-safe MCP input: `mcp-config`, `mcp-config-stdin`, `mcp-config-file` (create) | 172–174 | Same three-channel pattern as `custom-env`; `--mcp-config` warns about shell history / `ps`; value must be a JSON object or `null` | `goosar agent create --help` |
| MCP flags on `agent update` | 200–202 | Same three channels on update; `--mcp-config null` clears. Unlike `custom_env`, `mcp_config` IS settable via update | `goosar agent update --help` |
| `thinking-level` / `service-tier` flags on `agent update` | 189–190 | Thin pass-throughs; an explicit empty string clears the saved override and restores the runtime/local Codex default | `goosar agent update --help` |
| `runAgentCreate` builds body + `POST /api/agents` | 533–624 | Only sets a body key when the flag `Changed`; posts to `/api/agents` (line 614) | read 533–624 |
| Body assembly: description/instructions/runtime-config/custom-args/custom-env/mcp-config/model/thinking-level/service-tier | 548–608 | `model`, `thinking_level`, and `service_tier` are `Changed`-gated pass-throughs; omitted flags are not sent | read the `runAgentCreate` body assembly |
| `runAgentUpdate` sends `thinking_level` / `service_tier` / `mcp_config` | 627–718 | Each override key is added only when its flag is `Changed`; `custom_env` is intentionally not a flag here | read the `runAgentUpdate` body assembly |
| `parseMcpConfig` / `resolveMcpConfig` helpers | 1210, 1238 | Validator (object-or-`null`, content-free errors) + three-channel resolver, mirroring `parseCustomEnv`/`resolveCustomEnv` | read 1210–1294 |
| `agent skills set` = replace-all | 916 | `PUT /api/agents/{id}/skills` (934); `--skill-ids ''` clears all (922–925) | `goosar agent skills set --help` |
| `agent skills add` = additive | 941 | `POST /api/agents/{id}/skills/add` (962); requires ≥1 id (947–952) | `goosar agent skills add --help` |
| `agent skills list` | 884 | reads bindings, no side effect | `goosar agent skills list --help` |
| `agent env get` | 1018 | `GET /api/agents/{id}/env` (1028) | `goosar agent env get --help` |
| `agent env set` | 1053 | `PUT /api/agents/{id}/env` with full `custom_env` map (1073) | `goosar agent env set --help` |

## Copy command — `server/cmd/goosar/cmd_agent_copy.go`

| Contract | Line | Behavior | Safe check |
|---|---|---|---|
| `agentCopyCmd` (`copy <source-agent-id>`) + flag registrar | 21, 47, 54 | Own file with its own `init()` so `cmd_agent.go` line refs stay stable; `registerAgentCopyFlags` is shared with the tests | `goosar agent copy --help` |
| Reads source via `GET /api/agents/<id>` | 95 | Composes over existing endpoints — no dedicated copy API | read `runAgentCopy` |
| Same-runtime vs cross-runtime rule | 114, 187 | `sameRuntime` copies `model`/`thinking_level`/`service_tier`; a different `--runtime-id` drops them and requires `--model` (empty allowed) | `goosar agent copy --help` |
| Skills copied in the create transaction | 239 | Source skill ids sent as `skill_ids`, bound in the same `POST /api/agents` tx (267); `--no-skills` opts out | read `runAgentCopy` |
| Secrets never copied | 240–266 | `custom_env`/`mcp_config`/`runtime_config` set only from explicit secret-safe flags, never read from the source | `goosar agent copy --help` |

Note: the CLI no longer exposes `--from-template`. The agent-template backend
still exists (registry `server/internal/agenttmpl/`, handler `agent_template.go`,
routes `GET /api/agent-templates` and `POST /api/agents/from-template`, plus the
`packages/core` client/query wrappers) but is currently orphaned plumbing with no
live caller: the removed CLI flag was its only non-test consumer, and onboarding
does NOT use it — `packages/views/onboarding/steps/step-agent.tsx` builds four
hardcoded local presets (i18n-resolved) and creates via plain `POST /api/agents`
(`createAgent`), never `POST /api/agents/from-template`. Do not treat the template
API as a supported agent-creation path. This skill teaches manual `agent create`
only.

## Create handler — `server/internal/handler/agent.go`

| Contract | Line | Behavior |
|---|---|---|
| `maxAgentDescriptionLength = 255` | 31 | Cap is 255 **Unicode code points** (comment: counted via `utf8.RuneCountInString`, matches Postgres `char_length`) |
| `AgentResponse` omits plaintext `custom_env` | 33–53 | Exposes only `has_custom_env` (52) and `custom_env_key_count` (53); comment cites MUL-2600 |
| `CreateAgentRequest` fields | 930–970 | Includes `model`, `thinking_level`, and Codex `service_tier` alongside the profile/runtime/permission inputs |
| `name` required | 623–625 | 400 "name is required" |
| `description` ≤ 255 code points | 627–629 | `utf8.RuneCountInString(req.Description) > maxAgentDescriptionLength` → 400 |
| `runtime_id` required | 631–633 | `if req.RuntimeID == ""` → 400 "runtime_id is required" |
| `runtime_id` must resolve in workspace | 642–658 | parsed + `GetAgentRuntimeForWorkspace`; unknown → 400 "invalid runtime_id" |
| `thinking_level` provider-level validation | 896–903 | `!agent.IsKnownThinkingValue(runtime.Provider, req.ThinkingLevel)` → 400; fixed providers use an enum, Codex/OpenCode use safe-token syntax, and per-model gaps are deferred to daemon (MUL-2339) |
| `service_tier` provider-level validation | `agent.go` create/update paths | Non-empty values are Codex-only safe tokens; exact per-model support is daemon-owned |
| Defaults: `{}` config/env, `[]` args | 688–701 | `RuntimeConfig`→`{}`, `CustomEnv`→`{}`, `CustomArgs`→`[]` when nil, before insert |
| `visibility` default | 635–636 | `if req.Visibility == "" { req.Visibility = "private" }` — access-control field, not the runtime prompt |
| `max_concurrent_tasks` default | 638–639 | `if req.MaxConcurrentTasks == 0 { req.MaxConcurrentTasks = 6 }` — scheduler cap |
| `mcp_config` null-skip on create | 704–705 | raw JSON copied through unless the body value is the literal `null` |
| `mcp_config` allowlist policy | `internal/perimeterpolicy/mcp.go`; `agent.go` create/update mcp paths | `GOOSAR_MCP_ALLOWED_HOSTS` / `GOOSAR_MCP_ALLOWED_COMMANDS` (unset on the perimeter profile = corporate preset surface) → 400 naming the env var, validated on the plaintext BEFORE at-rest sealing; the daemon claim path additionally drops non-conforming entries with a warn |
| Provider allowlist policy | `internal/perimeterpolicy/providers.go`; `agent.go` create + runtime-move paths | `GOOSAR_ALLOWED_PROVIDERS` (unset on the perimeter profile = `hermes` only) → 403 on create/move to a runtime with a disallowed provider; both claim endpoints skip such runtimes; `/api/config` exposes `allowed_providers` (omitted when unrestricted) |
| `mcp_config` redacted on read | 54, 848–851 | `redactMcpConfig` sets `McpConfigRedacted=true`; a private agent read by a member also redacts (494, 509) |
| Who owns an agent | `isAgentOwner` `agent.go` 1509 | The single ownership predicate; `canViewAgentSecrets`, the invocation checks in `agent_access.go` and the runtime-binding gate all call it. `owner_id` is nullable and `uuidToString` answers `""` for NULL, so both empty sides are rejected before comparing — otherwise an ownerless agent would match a caller with no user id |
| Who may see `mcp_config` values | `canViewAgentSecrets` `agent.go` 1488 | The agent's own owner, nobody else. Workspace owner/admin lost this in GH #265; an ownerless legacy row (nullable `owner_id`) fails closed and is shown to no one, staying manageable through the write paths |
| Two reductions, one entry point | `applyMcpConfigVisibility` `agent.go` 1595 (`redactMcpConfig` 1556, `maskMcpConfig` 1578) | Agent actor or `always_redact_env` → field stripped to `null`. Any other non-owner → masked: same containers, same server names, each configuration replaced by `{"__goosar_masked__": true}` (`maskMcpConfigDocument` `agent_mcp_mask.go` 132). Both set `mcp_config_redacted: true`; used by ListAgents (916), GetAgent (980) and the runtime 409 body (`runtime.go` 929) |
| Masked write merge | `mergeMaskedMcpConfig` `agent_mcp_mask.go` 166; called from `UpdateAgent` `agent.go` 1835 | Resolves each placeholder against the stored (opened) document, so a non-owner deletes/adds/replaces one server without holding the others; keeps top-level keys masking dropped; an unresolvable placeholder is 400. `CreateAgent` rejects placeholders outright (`agent.go` 1183) — nothing to keep |
| Mutation responses redacted too | `redactAgentResponseForCaller` `agent.go` 1685; callers at 1320 (create), 2210 (update), 2308 (archive), 2344 (restore) | Applies the same rules to create/update/archive/restore bodies, kiosk switch included. Without it a no-op `PUT {"description":"..."}` returns another member's plaintext `mcp_config` and the read gate is cosmetic |
| Runtime binding is a secret channel | `runtimeDestinationAllowedForCaller` `agent.go` 1720; called from `UpdateAgent` 1906; payload built in `daemon.go` `buildClaimedTaskResponse` | The claim payload carries `custom_env` and the decrypted `mcp_config` in plaintext to whoever hosts the runtime, so a rebind is owner-only (GH #218): any non-owner `runtime_id` change is 403 regardless of destination. A no-op resubmit of the current `runtime_id` is tolerated |
| `always_redact_env` | `workspaceAlwaysRedactSecrets` `agent.go` 1450; workspace load `workspaceKioskRedactsMcpConfig` | Workspace setting read from `workspace.settings`; despite the name it governs `mcp_config` only and has never touched `custom_env`. On top of the owner gate its only remaining effect is a kiosk mode: the field is stripped for everyone, the agent's owner included, on reads AND on the four mutation responses. Not settable from any UI |
| `mcp_config` encrypted at rest | `internal/handler/mcp_secret.go` | When `GOOSAR_MCP_SECRET_KEY` is set, create/update seal the JSON into a `{"__goosar_sealed__": "..."}` envelope before insert and reads (API responses, daemon claim) open it back; legacy plaintext rows still read and a startup backfill seals them. Key unset -> plaintext storage with a startup warning. Transparent to the CLI/API contract |
| Qwen Code managed-MCP injection | `pkg/agent/qwen.go` | Non-null `mcp_config` is written to a daemon-owned 0600 temporary JSON file and passed with `--mcp-config`; the file is removed after the process exits, while `null` preserves native inheritance. |
| All providers skip disabled MCP entries | `pkg/agent/mcp_config.go` `mcpEntryDisabled` / `filterDisabledMcpServers`; `internal/daemon/runtime_mcp.go` `runtimeMcpEntryEnabled` | An entry with `"enabled": false` or `"disabled": true` is dropped at dispatch everywhere: ACP `session/new` (`buildACPMcpServers`), Claude/CodeBuddy/Qwen temp `--mcp-config` file (`writeMcpConfigToTemp`), Codex TOML block (`ensureCodexMcpConfig`), and the daemon runtime+agent merge (load and merge sides). Malformed flags fail open with a warning. Onboarding-seeded presets ship disabled until credentials are filled in |
| Workspace MCP library + assignment | `server/migrations/263_workspace_mcp_server.up.sql`; `internal/handler/workspace_mcp.go`, `workspace_mcp_api.go`; `pkg/db/queries/workspace_mcp.sql` | Two tables, no FK: `workspace_mcp_server` (library, sealed `config` + non-secret `transport`) and `agent_mcp_server` (assignment, own `enabled`). Routes: `/api/workspace-mcp-servers` (owner/admin write, member read, `RequireHumanActor`) and `/api/agents/{id}/mcp-servers` (workspace owner/admin only — assignment routes the decrypted config to the agent's machine, so the agent's owner alone does not qualify — plus an explicit agent-actor denial). No handler returns `config`; the listing queries do not even select it |
| Shared servers folded into a claim | `ResolveAgentMcpConfig` `internal/handler/workspace_mcp.go`; called from `buildClaimedTaskResponse` `daemon.go` between `openMcpConfig` and `mergeMCPOverlay` | Only servers ASSIGNED to this agent and left enabled (`ListEnabledAgentMcpServers`) are folded in, read fresh on every claim. Precedence: assigned workspace servers < the agent's own entries < the per-task overlay, then `MCPPolicy.FilterConfig`. The result is normalized onto `mcpServers` — the daemon's runtime merge reads the legacy `mcp` container only when `mcpServers` is absent, and only for opencode |
| Library entry rejects a toggle | `validateWorkspaceMcpServerEntry` `internal/handler/workspace_mcp.go` | `enabled` / `disabled` inside a library entry is 400. A disabled agent entry deletes the name from the daemon's merged map (`mergeRuntimeAndAgentMcpConfig`), which would also remove a same-named machine-local server the writer has no access to |
| Library config sealed fail-closed | `sealWorkspaceMcpEntry` `internal/handler/workspace_mcp_api.go`; `sealConfigDocument` `effective_config.go` | Uses the #214 config path, not legacy `sealMcpConfig`: with no `GOOSAR_MCP_SECRET_KEY` the write is 503, never plaintext. `transport` is computed from the plaintext BEFORE sealing and stored in its own non-secret column, so listing never opens a secretbox |
| Per-user credentials for a shared server | `server/migrations/270_workspace_mcp_user_credential.up.sql`, `272_deployment_mcp_server_credential_schema.up.sql`; `internal/handler/workspace_mcp_credentials.go`, `workspace_mcp_credentials_api.go` | The admin authors `credential_schema` on the library entry (field key + label + hint + required, non-secret, readable by any member); each user writes their OWN values through `PUT/DELETE /api/workspace-mcp-servers/{id}/credentials`, which take no user id, so nobody can read or write another person's. Values are sealed fail-closed and returned by no endpoint; listings carry only `provided_credentials` / `missing_credentials` key sets. A write is a PATCH onto the caller's existing set (an empty value clears one field). At claim time the AGENT OWNER's values are layered into the entry's `env`, winning over an admin-baked value of the same name; a missing REQUIRED field keeps the server out of the claim. Only stdio-shaped entries may declare a schema — an http/sse entry authenticates with `headers` and ignores `env`. The same layer applies to a DEPLOYMENT-library record a workspace enabled: the deployment admin authors its schema, every user supplies their own values against the same credentials endpoint |
| Both MCP tables swept on workspace delete | `DeleteWorkspace` `pkg/db/queries/workspace.sql` | Neither table carries an FK, so explicit CTEs remove the assignments (reached through the library rows, which are the only side that knows the workspace) and then the library itself |
| Random emoji avatar default | `agent_avatar.go` 11–32; `agent.go` 1127–1133 | Omitted, empty, or whitespace-only `avatar_url` becomes a cryptographically selected `emoji:<glyph>` sentinel; explicit values are preserved. The template handler uses the same helper at `agent_template.go` 458. |
| `CreateAgent` insert params | `agent.go` create path | Persists avatar_url, runtime_config, instructions, custom_env, custom_args, model, thinking_level, service_tier, mcp_config, visibility, max_concurrent_tasks |
| `UpdateAgent` rejects `custom_env` | 910–913 | if `custom_env` present in body → 400 "use PUT /api/agents/{id}/env (or `goosar agent env set`)" |
| `UpdateAgent` persists / clears `mcp_config` | 944–948, 1060–1061 | Tri-state from the raw body: key omitted → no change; literal `null` → `ClearAgentMcpConfig`; object → replace. No 400 like `custom_env` — `mcp_config` IS updatable here |
| `description` ≤ 255 on update too | 921–924 | same cap re-checked on update |

## Runtime model/thinking discovery — `server/pkg/agent/{models,thinking}.go`

| Contract | Line | Behavior |
|---|---|---|
| Codex model-list entry point | `models.go` 94–103 | `ListModels("codex")` uses cached daemon-local discovery instead of returning the fallback catalog unconditionally |
| Codex fallback catalog | `models.go` 301–354 | Used for Codex <0.122.0 and failed/malformed discovery; includes current verified visible models plus legacy `gpt-5.3-codex`, with a separate `Thinking` catalog on every model |
| Codex discovery version gate | `thinking.go` 280, 306–337 | `codex debug models --bundled` is used only for parseable versions ≥0.122.0; unsupported versions and command/parse/empty failures return the static model + thinking fallback |
| Codex catalog projection | `thinking.go` `parseCodexModelCatalog` | Hidden models are excluded; visible model, reasoning, and `service_tiers` metadata are preserved |
| Per-model thinking validation | `thinking.go` 547–640 | `ValidateThinkingLevel` accepts only values in the explicit model's `Thinking.SupportedLevels`; an empty Codex model fails closed because its effective `config.toml` model is unknown |
| Dynamic Codex token gate | `thinking.go` 642–710 | Server persistence accepts syntactically safe Codex tokens so new catalog values do not require a Goosar release; exact support remains a daemon-local per-model check |
| Per-model service-tier validation | `thinking.go` `ValidateServiceTier` | Accepts only a tier advertised for the explicit Codex model; empty model fails closed because config.toml is unknown |
| Daemon invalid-combination handling | `internal/daemon/daemon.go` 3860–3892 | Before execution, invalid `(provider, model, thinking_level)` combinations log a warning and omit the override rather than failing the task |

## Env endpoint — `server/internal/handler/agent_env.go`

| Contract | Line | Behavior |
|---|---|---|
| `authorizeAgentEnv` gate | 80 | loads agent, applies the checks below, and returns `isAgentOwner` — whether this caller is entitled to plaintext |
| Agent actors denied | 95–98 | `if actorType == "agent"` → 403 "agents may not access env management endpoints" (MUL-2600 impersonation guard) |
| Reachability gate | 100–109 | any workspace member passes `requireWorkspaceRole`, then the caller must be the agent's owner OR hold owner/admin; a plain member reaching somebody else's agent gets 403. The agent's own owner is admitted whatever their workspace role is |
| Plaintext gate | 106 | `canViewAgentSecrets(agent, userID)` — agent owner only (`agent.go` 1488) |
| Masked read | `GetAgentEnv` 135 | non-owner: values replaced by `****`, `values_masked: true`, `agent_env_listed` audit row. Owner: plaintext, `agent_env_revealed` with `revealed_keys`. Both are fail-closed on an audit-write failure: a listing discloses which integrations a member configured, and the UI promises every read is recorded |
| `maskEnvValues` | 207 | rewrites every value to the `****` sentinel, keys untouched |
| Placeholder writes rejected, not guessed | `mergeAgentEnv` 378 | `****` under a key with nothing stored → 400 (an older client's rename would otherwise delete both keys and report success); a value merely starting with `****` → 400 (an input pre-filled with the marker, typed onto). Errors name keys, never values |
| Non-owner value writes rejected | `UpdateAgentEnv` (GH #273) | after the merge, a non-owner whose request adds or changes any key gets 403 (error names keys only); preserving via `****` and removing keys stay allowed — same rule as `mergeMaskedMcpConfig` |
| Masked write response | `UpdateAgentEnv` ~309–320 | a non-owner's `PUT` echoes masked values, so `{"KEY":"****"}` cannot be used to read back what the `GET` hid |
| `custom_args` argv and safe launch log | `internal/daemon/daemon.go` `ExecOptions.CustomArgs`; `pkg/agent/command_log.go` `Config.logAgentCommand` | Custom args reach the provider process argv. Launch logs preserve flag names but redact inline values and positional/value tokens; OS process-list exposure remains, so credentials belong in `custom_env`. |
| Owner-only fields on `UpdateAgent` | `agent.go` `UpdateAgent` (GH #273/#218) | `instructions`, `custom_args`, `runtime_config` and `runtime_id` changes by a non-owner are 403; unchanged echoes are dropped/tolerated |

## Routes — `server/cmd/server/router.go`

| Contract | Line | Behavior |
|---|---|---|
| `GET /env` | 603 | `h.GetAgentEnv` (plaintext read, gated) |
| `PUT /env` | 604 | `h.UpdateAgentEnv` (full-map overwrite, gated) |

## Claim-time injection — `server/internal/handler/daemon.go`

| Contract | Line | Behavior |
|---|---|---|
| Fresh agent re-read on claim | 1109–1111 | `GetAgent(task.AgentID)` — claim uses persisted fields, not create output |
| Workspace skills FIRST | 1115 | `skills := h.TaskService.LoadAgentSkills(...)` |
| Built-ins appended | 1116 | `skills = append(skills, h.TaskService.BuiltinSkills()...)` |
| Runtime payload | `daemon.go` `TaskAgentData` | Carries `Instructions`, `Skills`, `CustomEnv`, `CustomArgs`, `Model`, `ThinkingLevel`, `ServiceTier`, and `McpConfig`; metadata-only fields remain absent |

## Skill loading — `server/internal/service/task.go`

| Contract | Line | Behavior |
|---|---|---|
| `LoadAgentSkills` | 1685 | `ListAgentSkills` + per-skill `ListSkillFiles` → content + supporting files for execution |

## Built-in skills — `server/internal/service/builtin_skills.go`

| Contract | Line | Behavior |
|---|---|---|
| `go:embed builtin_skills` | 10–11 | skills embedded at compile time |
| `loadBuiltinSkill` | 45 | reads `<name>/SKILL.md` (47) + walks sibling files into `Files` (56–68) |

## Persisted columns — `server/pkg/db/generated/agent.sql.go`

| Contract | Line | Behavior |
|---|---|---|
| `CreateAgent` INSERT | generated from `queries/agent.sql` | columns include `runtime_config, runtime_id, instructions, custom_env, custom_args, mcp_config, model, thinking_level, service_tier` |
| `CreateAgentParams` | generated from `queries/agent.sql` | typed params include nullable `Model`, `ThinkingLevel`, and `ServiceTier` |
| `UpdateAgent` SET | generated from `queries/agent.sql` | COALESCE updates include model/thinking/service tier; dedicated clear queries restore each nullable override |
| `UpdateAgentCustomEnv` (called by the `UpdateAgentEnv` handler) | 2652 | `SET custom_env = $2` — the only write path for env values |
