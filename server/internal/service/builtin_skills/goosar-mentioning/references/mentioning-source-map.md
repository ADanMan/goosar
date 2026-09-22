# Mentioning — source map

Every claim in `SKILL.md` traces to a line below. Re-derive against the current
tree before trusting any line number; the behavior is the contract, the line is
a pointer.

## The mention grammar (what parses)

| Fact | Source |
| --- | --- |
| `MentionRe` — the only recognizer of a mention link | `server/internal/util/mention.go:16` |
| Pattern: `` `\[@?(.+?)\]\(mention://(member\|agent\|squad\|issue\|all)/([0-9a-fA-F-]+\|all)\)` `` | `server/internal/util/mention.go:16` |
| `<type>` group = `member \| agent \| squad \| issue \| all` | `server/internal/util/mention.go:16` |
| `<id>` group = `[0-9a-fA-F-]+` (hex + dashes) **or** the literal `all` — so a typical name with non-hex letters never matches | `server/internal/util/mention.go:16` |
| `ParseMentions` extracts and dedups `{Type, ID}` from `m[2]`/`m[3]` | `server/internal/util/mention.go:24-37` |
| `Mention.Type` doc enum = "member", "agent", "issue", or "all" (squad added in regex) | `server/internal/util/mention.go:7` |
| `HasMentionAll` reports whether any parsed mention is `all` | `server/internal/util/mention.go:40-47` |

### Parser behavior tests (pin the example shapes the skill uses)

| Case proven | Source |
| --- | --- |
| `mention://member/<real-uuid>` parses to `{member, uuid}` | `server/internal/util/mention_test.go:42-45` |
| `mention://all/all` parses to `{all, all}` | `server/internal/util/mention_test.go:47-50` |
| `mention://agent/<uuid>` parses; label may contain `[brackets]` | `server/internal/util/mention_test.go:13-35` |
| plain text with no `mention://` parses to `nil` | `server/internal/util/mention_test.go:57-60` |
| Skill eval: a name where a UUID belongs (`mention://member/Alice`) parses to `nil`; a bare `@name` parses to `nil`; a real UUID parses; `@all` → `{all, all}`; a **wrong** type with a real UUID still parses (points at the wrong entity) | `server/internal/service/builtin_skills_test.go:101-157` |

## What each mention type enqueues

| Fact | Source |
| --- | --- |
| `computeCommentAgentTriggers` is the shared comment trigger computation used by preview and enqueueing | `server/internal/handler/comment.go:2169-2265` |
| `resolveMentionedAgentCommentTriggers` builds the explicit-mention trigger set; `enqueueCommentAgentTriggers` is the shared enqueue helper | `server/internal/handler/comment.go:2491-2660,1585` |
| Comment creation runs `triggerTasksForComment`, which computes triggers, applies suppressions, then enqueues | `server/internal/handler/comment.go:1503,1526-1541` |
| Comment edit re-triggering also runs `triggerTasksForComment` after cancelling old tasks for the edited comment | `server/internal/handler/comment.go:2818` |
| `squad` branch: resolve squad in workspace, read `LeaderID`, add the leader trigger | `server/internal/handler/comment.go:2564-2612` |
| `squad` → shared enqueue helper calls `EnqueueTaskForSquadLeader` | `server/internal/handler/comment.go:2109-2145` |
| Everything not `agent` after the squad branch is skipped: `if m.Type != "agent" { continue }` | `server/internal/handler/comment.go:2622-2624` |
| `agent` branch: load agent in workspace, then add the agent trigger | `server/internal/handler/comment.go:2625-2660` |
| `agent` → shared enqueue helper calls `EnqueueTaskForMention` (a run for that agent) | `server/internal/handler/comment.go:2130` |
| **`member` and `issue` mentions reach neither branch — they enqueue NOTHING.** A `member` mention fails the `!= "agent"` skip at lines 2622-2624 (the squad branch above it only matches `squad`); an `issue` mention does the same. | `server/internal/handler/comment.go:2564,2622-2624` |

## Preview and suppression

| Fact | Source |
| --- | --- |
| Preview route: `POST /api/issues/{id}/comments/trigger-preview` | `server/cmd/server/router.go:1415` |
| Preview handler loads the issue, expands issue identifiers, then calls `computeCommentAgentTriggers` | `server/internal/handler/comment.go:1153,1233` |
| Preview request accepts `content`, optional `parent_id`, and optional `editing_comment_id` | `server/internal/handler/comment.go:1018-1022` |
| Preview response returns agent `id`, `name`, optional `avatar_url`, `source`, and `reason` | `server/internal/handler/comment.go:1024-1040` |
| `editing_comment_id` is parsed as UUID input, scoped to the same workspace and issue, and used as `ExcludeTriggerCommentID` | `server/internal/handler/comment.go:1174-1187` |
| Preview validates or derives the parent context for an edit | `server/internal/handler/comment.go:1205-1230` |
| `CreateCommentRequest` accepts optional `suppress_agent_ids` | `server/internal/handler/comment.go:1009-1016` |
| `UpdateComment` accepts optional `suppress_agent_ids` | `server/internal/handler/comment.go:2709` |
| Create-comment `suppress_agent_ids` is parsed as request-boundary UUID input | `server/internal/handler/comment.go:1327` |
| Update-comment `suppress_agent_ids` is parsed as request-boundary UUID input | `server/internal/handler/comment.go:2733` |
| Create and edit trigger paths compute the full trigger set, then apply `filterSuppressedCommentAgentTriggers` before enqueueing | `server/internal/handler/comment.go:1531-1543,1545` |
| Frontend API sends `editing_comment_id` for preview and `suppress_agent_ids` for update when present | `packages/core/api/client.ts:664-700` |
| Edit UI calls preview with `editingCommentId`, renders trigger chips, tracks suppressed agents, and submits suppressions on save | `packages/views/issues/components/comment-card.tsx:269-274,300-315,359-367,578-582,858-862` |
| Preview hook includes `editingCommentId` in its query key and sends it to the API | `packages/views/issues/hooks/use-comment-trigger-preview.ts:58-80` |
| Timeline edit mutation passes suppressed agent IDs through to the API layer | `packages/views/issues/hooks/use-issue-timeline.ts:299-302` |

## Edit-preview pending-task dedup

| Fact | Source |
| --- | --- |
| Default dedup query skips any queued or dispatched task for the issue and agent | `server/pkg/db/queries/agent.sql:1190` |
| Edit-preview dedup query excludes only tasks whose `trigger_comment_id` equals the edited comment | `server/pkg/db/queries/agent.sql:1208` |
| `hasPendingTaskForIssueAndAgent` selects the comment-scoped exclusion only when `ExcludeTriggerCommentID` is valid | `server/internal/handler/comment.go:2472-2489` |
| Agent-assignee on-comment dedup uses the shared helper | `server/internal/handler/issue.go:2576-2594` |
| Assigned squad leader on-comment dedup uses the shared helper | `server/internal/handler/comment.go:2443-2467` |
| Mentioned squad leader dedup uses the shared helper | `server/internal/handler/comment.go:2613-2617` |
| Direct agent mention dedup uses the shared helper | `server/internal/handler/comment.go:2655-2659` |
| Positive regression test covers all four edit-preview trigger sources | `server/internal/handler/comment_trigger_preview_test.go:179-265` |
| Negative regression test proves another comment's pending task still dedupes the preview | `server/internal/handler/comment_trigger_preview_test.go:267-290` |
| Edit-submit regression test proves `suppress_agent_ids` filters update-triggered tasks | `server/internal/handler/comment_trigger_preview_test.go:292-316` |

## Guards that make a valid mention a silent no-op

| Guard | Source |
| --- | --- |
| agent archived / no runtime → `blockTarget` + `continue` (`ArchivedAt` set / `RuntimeID` invalid) | `server/internal/handler/comment.go:2647-2654` |
| squad leader archived / no runtime → `blockTarget` + `continue` | `server/internal/handler/comment.go:2605-2612` |
| private agent the actor cannot invoke → `blockTarget` + `continue` (`canInvokeAgent`) | `server/internal/handler/comment.go:2642-2645` |
| private squad leader the actor cannot invoke → `blockTarget` + `continue` (`canInvokeAgent`) | `server/internal/handler/comment.go:2600-2603` |
| already-pending dedup (agent) → shared pending-task helper | `server/internal/handler/comment.go:2655-2659` |
| already-pending dedup (squad leader) → shared pending-task helper | `server/internal/handler/comment.go:2613-2617` |
| `canAccessPrivateAgent` definition | `server/internal/handler/agent_access.go` (search `func (h *Handler) canAccessPrivateAgent`) |
| `canEnqueueSquadLeader` (loads leader, delegates to `canInvokeAgent`) | `server/internal/handler/agent_access.go:548` |
| autopilot-delegation invoke authority: an unattributed autopilot run delegating on the issue it created falls back to the autopilot creator as the effective invoking user for the gate, bound to verified speaking-task lineage (author == task agent, `task.issue_id` == this issue) so no cross-issue borrow (MUL-4857) | gate application via `opts.effectiveInvoker()` in `server/internal/handler/comment.go` (search `func (o commentTriggerComputeOptions) effectiveInvoker`); lineage-verifying helper in `server/internal/handler/agent_access.go` (search `func (h *Handler) autopilotDelegationAuthority`); resolved from the trusted X-Task-ID / `comment.source_task_id` via `autopilotDelegationAuthorityFromRequest` / `autopilotDelegationAuthorityFromComment` |
| autopilot-delegation authority on the DEFERRED path: a delegation to a busy target replays at the target's completion reconcile, which restores the same authority from `comment.source_task_id` (MUL-4857) | `server/internal/handler/daemon.go` (search `reconcileCommentsOnCompletion`, the `autopilotDelegationAuthorityFromComment` call) |
| authority lineage is persisted per-action: only an agent editing its OWN comment re-stamps `source_task_id` to the current editing task, issue-scoped via `commentSourceTaskIDForIssue` (requires `task.issue_id` == this issue); any other editor — including a workspace owner/admin editing an agent's comment (manage rights, not invoke rights) — CLEARS it, so a cross-issue edit or an admin edit makes every authority/originator read fail closed, including the deferred completion-reconcile — preview, save, and reconcile agree (MUL-4857) | `server/internal/handler/comment.go` (search `commentSourceTaskIDForIssue` and the `isAuthor` branch in `UpdateComment`) |
| on CREATE, the same issue-scoping applies to an issue task's own comment, but a CHAT task (no fixed issue — `task.issue_id` is always NULL) stamps `source_task_id` on ANY issue it comments on instead: `autopilotDelegationAuthority` independently re-verifies `task.issue_id == issue.id` on the freshly re-fetched task, which a chat task can never satisfy, so this only extends the originator-inheritance chain (`attributionFromComment`), never delegation authority (#122) | `server/internal/handler/comment.go` (search `case task.ChatSessionID.Valid` in `CreateComment`) |

## @all broadcast and assignee-trigger suppression

| Fact | Source |
| --- | --- |
| `@all` short-circuits the whole trigger computation: `computeCommentAgentTriggers` returns nil before explicit mentions are resolved, so a broadcast comment enqueues nothing — including explicit @agent/@squad mentions in the same comment | `server/internal/handler/comment.go:2188-2190` (`util.HasMentionAll` early return) |
| `HasMentionAll` helper | `server/internal/util/mention.go:39-47` |

## CLI id sources (where the UUID comes from)

| List command | Field used as mention id | Source |
| --- | --- | --- |
| `workspace member list` | `user_id` (NOT the membership-row id) | `server/cmd/goosar/cmd_workspace.go:465` |
| `agent list` | `id` | `server/cmd/goosar/cmd_agent.go:365` |
| `squad list` | `id` | `server/cmd/goosar/cmd_squad.go:57` |
| Member mention uses `user_id`, confirmed by the backend roster formatter: `formatMention(user.Name, "member", userID)` where `userID = UUIDToString(m.MemberID)` | `server/internal/handler/squad_briefing.go:189-190` |
| `formatMention` emits `[@<name>](mention://<type>/<id>)` | `server/internal/handler/squad_briefing.go:216-218` |

## Explicit non-claim: no member-notification path in the Go comment handler

The skill deliberately does **not** assert that a `member` mention "sends a
notification." `server/internal/handler/comment.go` has no notification
delivery path for member (or issue) mentions: `resolveMentionedAgentCommentTriggers`
branches only on `squad` and `agent`
(`server/internal/handler/comment.go:2564,2622-2624`), and a grep of the file for
`notif` returns only an unrelated comment about avoiding "log spam" on
unchanged threads — no member-notification call. The verified contract is
narrow: a `member` or `issue` mention renders as a link and enqueues no agent
run; only `agent` and `squad` mentions enqueue work. If a notification UX
exists, it is not in this handler, so this skill makes no claim about it.
