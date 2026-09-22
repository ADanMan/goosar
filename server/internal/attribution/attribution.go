// Пакет attribution реализует контракт «ответственный человек» для запусков задач
// агентов: каждая запись в agent_task_queue прослеживается ровно к одному человеку,
// и атрибуция объяснима — фиксируется уровень, на котором человек был определён.
package attribution

import "github.com/jackc/pgx/v5/pgtype"

type Source string

const (
	SourceDirectHuman Source = "direct_human"

	SourceDelegation Source = "delegation"

	SourceCommentSource Source = "comment_source"

	SourceTriggerOwner Source = "trigger_owner"

	SourceRuleOwner Source = "rule_owner"

	SourceOwnerFallback Source = "owner_fallback"

	SourceBackfill Source = "backfill"

	SourceUnattributed Source = "unattributed"
)

func (src Source) Precise() bool {
	switch src {
	case SourceDirectHuman, SourceDelegation, SourceCommentSource, SourceTriggerOwner, SourceRuleOwner:
		return true
	default:
		return false
	}
}

func (src Source) String() string {
	if src == "" {
		return string(SourceUnattributed)
	}
	return string(src)
}

type EvidenceKind string

const (
	EvidenceComment         EvidenceKind = "comment"
	EvidenceIssueAssignment EvidenceKind = "issue_assignment"
	EvidenceAutopilotRun    EvidenceKind = "autopilot_run"
	EvidenceRuleVersion     EvidenceKind = "rule_version"
	EvidenceRerun           EvidenceKind = "rerun"

	EvidenceChat EvidenceKind = "chat"
)

type TriggerKind string

const (
	KindMemberComment     TriggerKind = "member_comment"
	KindMemberMention     TriggerKind = "member_mention"
	KindMemberAssign      TriggerKind = "member_assign"
	KindAgentMention      TriggerKind = "agent_mention"
	KindAgentComment      TriggerKind = "agent_comment"
	KindSubIssueCreate    TriggerKind = "sub_issue_create"
	KindStageWakeup       TriggerKind = "stage_wakeup"
	KindQuickCreate       TriggerKind = "quick_create"
	KindChat              TriggerKind = "chat"
	KindAutopilotSchedule TriggerKind = "autopilot_schedule"
	KindAutopilotWebhook  TriggerKind = "autopilot_webhook"
	KindAutopilotManual   TriggerKind = "autopilot_manual"
	KindRetry             TriggerKind = "retry"
	KindRerun             TriggerKind = "rerun"
	KindDeferredFallback  TriggerKind = "deferred_fallback"
)

type Result struct {
	UserID              pgtype.UUID
	AccountableUserID   pgtype.UUID
	Source              Source
	DelegatedFromTaskID pgtype.UUID
	RuleVersionID       pgtype.UUID
	RetryOfTaskID       pgtype.UUID
	RerunOfTaskID       pgtype.UUID
	EvidenceKind        EvidenceKind
	EvidenceRefID       pgtype.UUID
}

func finalizeAttribution(r Result) Result {
	if r.UserID.Valid {
		r.AccountableUserID = r.UserID
	}
	return r
}

type CommentFacts struct {
	CommentID  pgtype.UUID
	AuthorType string
	AuthorID   pgtype.UUID

	SourceTaskID     pgtype.UUID
	ParentOriginator pgtype.UUID

	ParentAccountable pgtype.UUID
}

func ClassifyComment(f CommentFacts, agentAuthoredSource Source) Result {
	switch f.AuthorType {
	case "member":
		return finalizeAttribution(Result{
			UserID:        f.AuthorID,
			Source:        SourceDirectHuman,
			EvidenceKind:  EvidenceComment,
			EvidenceRefID: f.CommentID,
		})
	case "agent":
		r := Result{EvidenceKind: EvidenceComment, EvidenceRefID: f.CommentID}
		if !f.SourceTaskID.Valid {

			r.Source = SourceUnattributed
			return finalizeAttribution(r)
		}
		r.DelegatedFromTaskID = f.SourceTaskID
		if f.ParentOriginator.Valid {
			r.UserID = f.ParentOriginator
			r.Source = agentAuthoredSource
		} else if f.ParentAccountable.Valid {

			r.AccountableUserID = f.ParentAccountable
			r.Source = agentAuthoredSource
		} else {

			r.Source = SourceUnattributed
		}
		return finalizeAttribution(r)
	default:
		return finalizeAttribution(Result{Source: SourceUnattributed, EvidenceKind: EvidenceComment, EvidenceRefID: f.CommentID})
	}
}

type DirectFacts struct {
	IssueID     pgtype.UUID
	CreatorType string
	CreatorID   pgtype.UUID

	ActorUserID pgtype.UUID

	OriginType       string
	OriginTaskID     pgtype.UUID
	OriginOriginator pgtype.UUID

	OriginAccountable pgtype.UUID
}

func ClassifyDirect(f DirectFacts) Result {

	if f.ActorUserID.Valid {
		return finalizeAttribution(Result{
			UserID:        f.ActorUserID,
			Source:        SourceDirectHuman,
			EvidenceKind:  EvidenceIssueAssignment,
			EvidenceRefID: f.IssueID,
		})
	}
	if f.CreatorType == "member" && f.CreatorID.Valid {
		return finalizeAttribution(Result{
			UserID:        f.CreatorID,
			Source:        SourceDirectHuman,
			EvidenceKind:  EvidenceIssueAssignment,
			EvidenceRefID: f.IssueID,
		})
	}
	switch f.OriginType {
	case "quick_create", "agent_create":
		r := Result{
			DelegatedFromTaskID: f.OriginTaskID,
			EvidenceKind:        EvidenceIssueAssignment,
			EvidenceRefID:       f.IssueID,
		}
		if f.OriginOriginator.Valid {
			r.UserID = f.OriginOriginator
			r.Source = SourceDelegation
		} else if f.OriginAccountable.Valid {

			r.AccountableUserID = f.OriginAccountable
			r.Source = SourceDelegation
		} else {
			r.Source = SourceUnattributed
		}
		return finalizeAttribution(r)
	default:
		return finalizeAttribution(Result{Source: SourceUnattributed, EvidenceKind: EvidenceIssueAssignment, EvidenceRefID: f.IssueID})
	}
}

func DirectHumanRun(userID pgtype.UUID, evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	if !userID.Valid {
		return finalizeAttribution(Result{Source: SourceUnattributed, EvidenceKind: evidenceKind, EvidenceRefID: evidenceRefID})
	}
	return finalizeAttribution(Result{
		UserID:        userID,
		Source:        SourceDirectHuman,
		EvidenceKind:  evidenceKind,
		EvidenceRefID: evidenceRefID,
	})
}

func Unattributed(evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	return finalizeAttribution(Result{Source: SourceUnattributed, EvidenceKind: evidenceKind, EvidenceRefID: evidenceRefID})
}

func RuleOwner(publisherUserID, ruleVersionID pgtype.UUID, evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	r := Result{
		RuleVersionID: ruleVersionID,
		EvidenceKind:  evidenceKind,
		EvidenceRefID: evidenceRefID,
	}
	if publisherUserID.Valid {
		r.Source = SourceRuleOwner
		r.AccountableUserID = publisherUserID
	} else {
		r.Source = SourceUnattributed
	}
	return finalizeAttribution(r)
}

func TriggerOwner(creatorUserID pgtype.UUID, evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	r := Result{
		EvidenceKind:  evidenceKind,
		EvidenceRefID: evidenceRefID,
	}
	if creatorUserID.Valid {
		r.Source = SourceTriggerOwner
		r.AccountableUserID = creatorUserID
	} else {
		r.Source = SourceUnattributed
	}
	return finalizeAttribution(r)
}

func OwnerFallback(r Result, ownerUserID pgtype.UUID) Result {
	if r.Source != SourceUnattributed || !ownerUserID.Valid {
		return r
	}
	r.Source = SourceOwnerFallback
	r.AccountableUserID = ownerUserID
	return r
}
