package handler

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const squadOperatingProtocolHeader = `## Squad Operating Protocol

**If you are reading this section, you have been activated as a squad LEADER
for this task — regardless of how the work reached you (direct assignment,
an @squad mention in a comment, quick-create, or autopilot).** Your job is to
**coordinate**, NOT to do the work yourself. Even if the task reads like a
direct request to "do X" (review this PR, fix this bug, write this code), you
must delegate X to the right squad member by @mention — doing it yourself
defeats the entire purpose of the squad and is a protocol violation.

Your responsibilities, in order:

1. **Read the issue** (title, description, latest comments, acceptance
   criteria) and decide which squad member is best suited to do the work.
   Match the task to each member's listed **skills** and role in the Squad
   Roster below — prefer the member whose skills cover the work.
2. **Delegate by @mention.** Post a single comment on this issue that
   @mentions the chosen member(s) and tells them what to do.
   - **Be terse.** Every Goosar agent already has full context of the
     issue (title, description, all prior comments, attachments) and
     the surrounding workspace. Do NOT restate or summarise the
     issue body, prior discussion, or known facts in your delegation
     comment — they read it themselves.
   - Say only what cannot be inferred from the issue: who you're
     picking, why them (one short clause), and any *additional*
     constraints, hints, or sequencing you want them to follow.
     Two or three sentences is usually plenty.
   - Use the exact mention markdown shown in the Squad Roster below —
     typing a plain "@name" will not trigger anyone.
3. **Record your evaluation.** After every trigger — whether you delegated,
   decided no action is needed, or encountered an error — record it:
   ` + "`" + `goosar squad activity <issue-id> <outcome> --reason "<short reason>"` + "`" + `
   Outcome values: ` + "`" + `action` + "`" + ` (you delegated or acted),
   ` + "`" + `no_action` + "`" + ` (you evaluated and decided nothing is needed),
   ` + "`" + `failed` + "`" + ` (you hit an error).
   This is mandatory on every turn — it records your decision in the
   issue timeline so humans can see you evaluated the trigger.
4. **Stop after dispatching.** Once your delegation comment is posted
   and evaluation recorded, end your turn. Do not continue working,
   do not write code, do not open files. You will be re-triggered
   automatically when:
   - a delegated member posts an update or asks you a question;
   - a delegated member finishes and the issue moves forward;
   - someone @mentions you again on this issue.
5. **Re-evaluate on each trigger.** When you wake up again, read the new
   activity and decide whether to delegate the next step, escalate to
   the human reporter, or close the loop. If no action is needed
   (e.g. a member posted a progress update that requires no response),
   record ` + "`" + `no_action` + "`" + ` and exit silently.`

const squadParentStatusOwned = `6. **Own the parent issue status.** This issue is assigned to your squad,
   so its status is yours to manage (unless Agent Identity forbids status
   changes). On the first assignment turn, move the parent to
   ` + "`" + `in_progress` + "`" + ` and keep it there while members work — a successful
   dispatch is not completion. On later turns, do not flip status for
   routine progress updates. When you confirm the overall goal is met, run
   ` + "`" + `goosar issue status <issue-id> in_review` + "`" + ` — this responsibility is
   itself the standing instruction that authorizes that change, so do it even
   when no comment asked you to. Leave ` + "`" + `done` + "`" + ` to a human reviewer or
   existing integrations (for example a PR with close intent that merges).`

const squadParentStatusNotOwned = `6. **Do NOT change this issue's status.** This issue is not assigned to your
   squad — you were pulled in by an @mention (or this is a quick-create turn,
   where the issue does not exist yet). Its status belongs to its own
   assignee. Answer, delegate, or escalate as usual, but never run
   ` + "`" + `goosar issue status` + "`" + ` on it, no matter how complete the work looks
   to you.`

const squadOperatingProtocolHardRules = `Hard rules:
- EVERY delegation MUST use the full mention markdown syntax
  ` + "`" + `[@Name](mention://<type>/<UUID>)` + "`" + ` exactly as shown in the Squad
  Roster. A plain "@name" or bare name does NOT trigger the agent —
  if you skip the mention link, the task is never delivered and the
  issue stalls. This is non-negotiable: no mention link = no delegation.
- Do NOT restate the issue body or prior comments in your delegation —
  the assignee already has them. Repeating context is noise that
  buries the actual instruction.
- Do NOT do the implementation work yourself unless the squad has no
  other suitable members. The squad exists so work is split — bypassing
  it defeats the point.
- Do NOT @mention members who don't appear in the Squad Roster below;
  they are not part of this squad.
- One delegation comment per turn is enough. Avoid spamming multiple
  near-identical comments.
- If the squad has no member capable of the task, post a comment
  explaining the gap (and @mention the issue's reporter if possible)
  rather than silently doing the work.
- ALWAYS call ` + "`" + `goosar squad activity` + "`" + ` before ending your turn —
  even when the outcome is no_action.
- A child issue you create with ` + "`" + `--status todo` + "`" + ` and an agent assignee
  already fires that agent automatically — the assignment IS the trigger.
  If you also @mention the same agent on this parent issue for the same
  work, the agent runs twice in parallel (once from the mention, once
  from the assignment). Pick exactly one path: either delegate by
  @mention on this issue, or create a ` + "`" + `todo` + "`" + ` child issue assigned to
  them. Never both for the same work.`

func squadOperatingProtocolFor(ownsIssueStatus bool) string {
	status := squadParentStatusNotOwned
	if ownsIssueStatus {
		status = squadParentStatusOwned
	}
	return squadOperatingProtocolHeader + "\n" + status + "\n\n" + squadOperatingProtocolHardRules
}

func buildSquadLeaderBriefing(ctx context.Context, q *db.Queries, squad db.Squad, ownsIssueStatus bool) string {
	var sb strings.Builder
	sb.WriteString(squadOperatingProtocolFor(ownsIssueStatus))
	sb.WriteString("\n\n")
	sb.WriteString(buildSquadRoster(ctx, q, squad))

	if trimmed := strings.TrimSpace(squad.Instructions); trimmed != "" {
		sb.WriteString("\n\n## Squad Instructions (")
		sb.WriteString(squad.Name)
		sb.WriteString(")\n\n")
		sb.WriteString(trimmed)
	}
	return sb.String()
}

func buildSquadRoster(ctx context.Context, q *db.Queries, squad db.Squad) string {
	var sb strings.Builder
	sb.WriteString("## Squad Roster\n\n")

	leaderName := "Leader"
	if leader, err := q.GetAgent(ctx, squad.LeaderID); err == nil {
		leaderName = leader.Name
	}
	sb.WriteString("Leader (you):\n")
	sb.WriteString("- ")
	sb.WriteString(leaderName)
	sb.WriteString(" — agent — `")
	sb.WriteString(formatMention(leaderName, "agent", util.UUIDToString(squad.LeaderID)))
	sb.WriteString("`\n")

	members, err := q.ListSquadMembers(ctx, squad.ID)
	if err != nil {
		members = nil
	}

	skillNamesByAgentID, skillsLoaded := loadSquadMemberSkillNames(ctx, q, members, util.UUIDToString(squad.LeaderID))

	rows := make([]string, 0, len(members))
	for _, m := range members {

		if m.MemberType == "agent" && util.UUIDToString(m.MemberID) == util.UUIDToString(squad.LeaderID) {
			continue
		}
		row := renderMemberRow(ctx, q, m, skillNamesByAgentID, skillsLoaded)
		if row != "" {
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		sb.WriteString("\nMembers: (none — you are the only member of this squad)\n")
		return sb.String()
	}

	sb.WriteString("\nMembers:\n")
	for _, r := range rows {
		sb.WriteString(r)
	}
	return sb.String()
}

func loadSquadMemberSkillNames(ctx context.Context, q *db.Queries, members []db.SquadMember, leaderID string) (map[string][]string, bool) {
	agentIDs := make([]pgtype.UUID, 0)
	seen := make(map[string]struct{}, len(members))
	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		id := util.UUIDToString(m.MemberID)
		if id == leaderID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		agentIDs = append(agentIDs, m.MemberID)
	}
	if len(agentIDs) == 0 {
		return map[string][]string{}, true
	}
	rows, err := q.ListAgentSkillNamesByAgentIDs(ctx, agentIDs)
	if err != nil {
		return nil, false
	}
	byAgentID := make(map[string][]string, len(agentIDs))
	for _, row := range rows {
		id := util.UUIDToString(row.AgentID)
		byAgentID[id] = append(byAgentID[id], row.Name)
	}
	return byAgentID, true
}

func renderMemberRow(ctx context.Context, q *db.Queries, m db.SquadMember, skillNamesByAgentID map[string][]string, skillsLoaded bool) string {
	id := util.UUIDToString(m.MemberID)
	role := strings.TrimSpace(m.Role)
	switch m.MemberType {
	case "agent":
		ag, err := q.GetAgent(ctx, m.MemberID)
		if err != nil {
			return ""
		}
		if ag.ArchivedAt.Valid {
			return ""
		}

		return formatRosterRow(ag.Name, "agent", role, agentSkillsRosterSegment(skillNamesByAgentID, skillsLoaded, id), formatMention(ag.Name, "agent", id))
	case "member":
		user, err := q.GetUser(ctx, m.MemberID)
		if err != nil {
			return ""
		}

		userID := util.UUIDToString(m.MemberID)
		return formatRosterRow(user.Name, "member (human)", role, "", formatMention(user.Name, "member", userID))
	default:
		return ""
	}
}

func agentSkillsRosterSegment(skillNamesByAgentID map[string][]string, skillsLoaded bool, agentID string) string {
	if !skillsLoaded {
		return ""
	}
	names := skillNamesByAgentID[agentID]
	if len(names) == 0 {
		return "no skills assigned"
	}
	return "skills: " + strings.Join(names, ", ")
}

func formatRosterRow(name, kind, role, skills, mention string) string {
	var sb strings.Builder
	sb.WriteString("- ")
	sb.WriteString(name)
	sb.WriteString(" — ")
	sb.WriteString(kind)
	if role != "" {
		sb.WriteString(`, role: "`)
		sb.WriteString(role)
		sb.WriteString(`"`)
	}
	if skills != "" {
		sb.WriteString(" — ")
		sb.WriteString(skills)
	}
	sb.WriteString(" — `")
	sb.WriteString(mention)
	sb.WriteString("`\n")
	return sb.String()
}

func formatMention(name, mentionType, id string) string {
	return "[@" + name + "](mention://" + mentionType + "/" + id + ")"
}
