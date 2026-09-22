package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/attribution"
	"github.com/adanman/goosar/server/internal/dispatch"
	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/issueguard"
	"github.com/adanman/goosar/server/internal/issueposition"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type AutopilotService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Bus       *events.Bus
	TaskSvc   *TaskService
}

const DefaultAutopilotTriggerTimezone = "UTC"

const autopilotRecentDuplicateWindow = 60 * time.Second

func NewAutopilotService(q *db.Queries, tx TxStarter, bus *events.Bus, taskSvc *TaskService) *AutopilotService {
	return &AutopilotService{Queries: q, TxStarter: tx, Bus: bus, TaskSvc: taskSvc}
}

type autopilotRuleConfigSummary struct {
	AssigneeType  string `json:"assignee_type"`
	AssigneeID    string `json:"assignee_id"`
	Status        string `json:"status"`
	ExecutionMode string `json:"execution_mode"`
}

func RecordAutopilotRuleVersion(ctx context.Context, q *db.Queries, ap db.Autopilot, publishedByType string, publishedByID pgtype.UUID) error {
	summary, err := json.Marshal(autopilotRuleConfigSummary{
		AssigneeType:  ap.AssigneeType,
		AssigneeID:    util.UUIDToString(ap.AssigneeID),
		Status:        ap.Status,
		ExecutionMode: ap.ExecutionMode,
	})
	if err != nil {
		return fmt.Errorf("marshal rule version config summary: %w", err)
	}
	if _, err := q.CreateAutopilotRuleVersion(ctx, db.CreateAutopilotRuleVersionParams{
		AutopilotID:     ap.ID,
		WorkspaceID:     ap.WorkspaceID,
		PublishedByType: publishedByType,
		PublishedByID:   publishedByID,
		ConfigSummary:   summary,
	}); err != nil {
		return fmt.Errorf("create autopilot rule version: %w", err)
	}
	return nil
}

func (s *AutopilotService) DispatchAutopilot(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
) (*db.AutopilotRun, error) {

	run, _, err := s.dispatchAutopilot(ctx, autopilot, triggerID, source, payload, pgtype.Timestamptz{}, pgtype.UUID{}, pgtype.UUID{})
	return run, err
}

func (s *AutopilotService) DispatchAutopilotManual(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	payload []byte,
	actorUserID pgtype.UUID,
) (*db.AutopilotRun, dispatch.ReasonCode, error) {

	return s.dispatchAutopilot(ctx, autopilot, triggerID, "manual", payload, pgtype.Timestamptz{}, pgtype.UUID{}, actorUserID)
}

func (s *AutopilotService) AdmitAutopilotWebhookDelivery(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	payload []byte,
	deliveryID pgtype.UUID,
) (*db.AutopilotRun, error) {
	if !deliveryID.Valid {
		return nil, fmt.Errorf("admit webhook delivery: delivery_id is required")
	}

	existing, err := s.Queries.GetAutopilotRunByWebhookDelivery(ctx, deliveryID)
	switch {
	case err == nil:
		return &existing, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("admit webhook delivery: lookup existing run: %w", err)
	}

	if reason, _, skip := s.shouldSkipDispatch(ctx, autopilot, pgtype.UUID{}); skip {
		run, err := s.recordSkippedRun(
			ctx,
			autopilot,
			triggerID,
			"webhook",
			payload,
			pgtype.Timestamptz{},
			deliveryID,
			reason,
		)
		if err != nil {
			return s.recoverConcurrentWebhookAdmission(
				ctx,
				deliveryID,
				fmt.Errorf("admit webhook delivery: create skipped run: %w", err),
			)
		}
		return run, nil
	}

	initialStatus := "issue_created"
	if autopilot.ExecutionMode == "run_only" {
		initialStatus = "running"
	}
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:       autopilot.ID,
		TriggerID:         triggerID,
		Source:            "webhook",
		Status:            initialStatus,
		TriggerPayload:    payload,
		SquadID:           autopilotSquadAttribution(autopilot),
		WebhookDeliveryID: deliveryID,
	})
	if err != nil {
		return s.recoverConcurrentWebhookAdmission(
			ctx,
			deliveryID,
			fmt.Errorf("admit webhook delivery: create run: %w", err),
		)
	}
	s.captureAutopilotRunStarted(autopilot, run, "webhook")
	return &run, nil
}

func (s *AutopilotService) recoverConcurrentWebhookAdmission(
	ctx context.Context,
	deliveryID pgtype.UUID,
	cause error,
) (*db.AutopilotRun, error) {

	var pgErr *pgconn.PgError
	if !errors.As(cause, &pgErr) || pgErr.Code != "23505" {
		return nil, cause
	}
	existing, err := s.Queries.GetAutopilotRunByWebhookDelivery(ctx, deliveryID)
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("admit webhook delivery: reload concurrent run: %w", err)
	}
	return nil, cause
}

func (s *AutopilotService) DispatchAutopilotForWebhookDelivery(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	payload []byte,
	deliveryID pgtype.UUID,
) (*db.AutopilotRun, error) {
	run, err := s.AdmitAutopilotWebhookDelivery(ctx, autopilot, triggerID, payload, deliveryID)
	if err != nil {
		return nil, err
	}
	if isAutopilotRunComplete(*run) {
		if autopilot.ExecutionMode == "create_issue" && run.IssueID.Valid {
			if repairErr := s.ensureWebhookCreateIssueTask(ctx, autopilot, *run); repairErr != nil {
				return run, repairErr
			}
		}
		return run, nil
	}

	if autopilot.ExecutionMode == "run_only" && !run.TaskID.Valid {
		task, taskErr := s.Queries.GetAutopilotTaskByRun(ctx, run.ID)
		switch {
		case taskErr == nil:
			updated, updateErr := s.Queries.UpdateAutopilotRunRunning(ctx, db.UpdateAutopilotRunRunningParams{
				ID:     run.ID,
				TaskID: task.ID,
			})
			if updateErr != nil {
				return run, fmt.Errorf("dispatch for webhook delivery: repair task linkage: %w", updateErr)
			}
			s.TaskSvc.NotifyTaskEnqueued(ctx, task)
			return &updated, nil
		case !errors.Is(taskErr, pgx.ErrNoRows):
			return run, fmt.Errorf("dispatch for webhook delivery: lookup linked task: %w", taskErr)
		}
	}

	dispatched, _, err := s.dispatchAutopilotRun(ctx, autopilot, triggerID, "webhook", run, pgtype.UUID{})
	return dispatched, err
}

func (s *AutopilotService) ensureWebhookCreateIssueTask(ctx context.Context, autopilot db.Autopilot, run db.AutopilotRun) error {
	tasks, err := s.Queries.ListTasksByIssue(ctx, run.IssueID)
	if err != nil {
		return fmt.Errorf("dispatch for webhook delivery: inspect issue tasks: %w", err)
	}
	if len(tasks) > 0 {
		return nil
	}
	issue, err := s.Queries.GetIssue(ctx, run.IssueID)
	if err != nil {
		return fmt.Errorf("dispatch for webhook delivery: load linked issue: %w", err)
	}
	if issue.Status != "todo" && issue.Status != "in_progress" {
		return nil
	}
	if autopilot.AssigneeType == "squad" {
		leader, _, err := s.resolveAutopilotLeader(ctx, autopilot)
		if err != nil {
			return fmt.Errorf("dispatch for webhook delivery: resolve squad leader: %w", err)
		}
		if _, err := s.TaskSvc.EnqueueTaskForSquadLeader(ctx, issue, leader.ID, autopilot.AssigneeID, pgtype.UUID{}); err != nil {
			return fmt.Errorf("dispatch for webhook delivery: repair squad task: %w", err)
		}
		return nil
	}
	if _, err := s.TaskSvc.EnqueueTaskForIssue(ctx, issue); err != nil {
		return fmt.Errorf("dispatch for webhook delivery: repair issue task: %w", err)
	}
	return nil
}

func (s *AutopilotService) DispatchAutopilotForPlan(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
	plannedAt time.Time,
) (*db.AutopilotRun, error) {
	if !triggerID.Valid {
		return nil, fmt.Errorf("dispatch for plan: trigger_id is required")
	}
	if plannedAt.IsZero() {
		return nil, fmt.Errorf("dispatch for plan: planned_at is required")
	}
	plannedTS := pgtype.Timestamptz{Time: plannedAt.UTC(), Valid: true}

	existing, err := s.Queries.GetAutopilotRunByTriggerAndPlanned(ctx, db.GetAutopilotRunByTriggerAndPlannedParams{
		TriggerID: triggerID,
		PlannedAt: plannedTS,
	})
	switch {
	case err == nil && isAutopilotRunComplete(existing):

		return &existing, nil

	case err == nil:

		slog.Warn("autopilot dispatch for plan: recovering partial run",
			"run_id", util.UUIDToString(existing.ID),
			"trigger_id", util.UUIDToString(triggerID),
			"planned_at", plannedAt.UTC().Format(time.RFC3339),
			"status", existing.Status,
			"issue_set", existing.IssueID.Valid,
			"task_set", existing.TaskID.Valid,
		)
		if err := s.Queries.RecoverPartialAutopilotRun(ctx, existing.ID); err != nil {
			return nil, fmt.Errorf("dispatch for plan: recover partial run: %w", err)
		}

	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("dispatch for plan: lookup existing run: %w", err)
	}

	run, _, err := s.dispatchAutopilot(ctx, autopilot, triggerID, source, payload, plannedTS, pgtype.UUID{}, pgtype.UUID{})
	return run, err
}

func isAutopilotRunComplete(run db.AutopilotRun) bool {
	switch run.Status {
	case "completed", "failed", "skipped":
		return true
	case "issue_created":
		return run.IssueID.Valid
	case "running":
		return run.TaskID.Valid
	default:
		return false
	}
}

func (s *AutopilotService) dispatchAutopilot(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
	plannedAt pgtype.Timestamptz,
	webhookDeliveryID pgtype.UUID,
	actorUserID pgtype.UUID,
) (*db.AutopilotRun, dispatch.ReasonCode, error) {
	if reason, code, skip := s.shouldSkipDispatch(ctx, autopilot, actorUserID); skip {
		run, err := s.recordSkippedRun(ctx, autopilot, triggerID, source, payload, plannedAt, webhookDeliveryID, reason)
		return run, code, err
	}

	initialStatus := "issue_created"
	if autopilot.ExecutionMode == "run_only" {
		initialStatus = "running"
	}

	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:       autopilot.ID,
		TriggerID:         triggerID,
		Source:            source,
		Status:            initialStatus,
		TriggerPayload:    payload,
		SquadID:           autopilotSquadAttribution(autopilot),
		PlannedAt:         plannedAt,
		WebhookDeliveryID: webhookDeliveryID,
	})
	if err != nil {
		return nil, dispatch.ReasonInternalError, fmt.Errorf("create run: %w", err)
	}
	s.captureAutopilotRunStarted(autopilot, run, source)
	return s.dispatchAutopilotRun(ctx, autopilot, triggerID, source, &run, actorUserID)
}

func (s *AutopilotService) dispatchAutopilotRun(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	run *db.AutopilotRun,
	actorUserID pgtype.UUID,
) (*db.AutopilotRun, dispatch.ReasonCode, error) {
	switch autopilot.ExecutionMode {
	case "create_issue":
		triggerTimezone := s.resolveAutopilotTriggerTimezone(ctx, triggerID)
		if err := s.dispatchCreateIssue(ctx, autopilot, run, triggerTimezone, actorUserID); err != nil {
			if skipped, code := s.handleDispatchSkip(ctx, autopilot, run, err); skipped != nil {
				return skipped, code, nil
			}
			s.failRun(ctx, run.ID, err.Error())
			s.captureAutopilotRunFailed(autopilot, *run, source, err.Error())
			return run, dispatchFailReasonCode(err), fmt.Errorf("dispatch create_issue: %w", err)
		}
	case "run_only":
		if err := s.dispatchRunOnly(ctx, autopilot, run, actorUserID); err != nil {
			if skipped, code := s.handleDispatchSkip(ctx, autopilot, run, err); skipped != nil {
				return skipped, code, nil
			}
			s.failRun(ctx, run.ID, err.Error())
			s.captureAutopilotRunFailed(autopilot, *run, source, err.Error())
			return run, dispatchFailReasonCode(err), fmt.Errorf("dispatch run_only: %w", err)
		}
	default:
		s.failRun(ctx, run.ID, "unknown execution_mode: "+autopilot.ExecutionMode)
		s.captureAutopilotRunFailed(autopilot, *run, source, "unknown execution_mode: "+autopilot.ExecutionMode)
		return run, dispatch.ReasonInternalError, fmt.Errorf("unknown execution_mode: %s", autopilot.ExecutionMode)
	}

	s.Queries.UpdateAutopilotLastRunAt(ctx, autopilot.ID)

	s.Bus.Publish(events.Event{
		Type:        protocol.EventAutopilotRunStart,
		WorkspaceID: util.UUIDToString(autopilot.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"run_id":       util.UUIDToString(run.ID),
			"autopilot_id": util.UUIDToString(autopilot.ID),
			"source":       source,
			"status":       run.Status,
		},
	})

	return run, "", nil
}

func dispatchFailReasonCode(err error) dispatch.ReasonCode {
	if errors.Is(err, ErrAttributionFailClosed) {
		return dispatch.ReasonAttributionBlocked
	}
	return dispatch.ReasonInternalError
}

func (s *AutopilotService) dispatchCreateIssue(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, triggerTimezone string, actorUserID pgtype.UUID) error {
	leader, _, err := s.resolveAutopilotLeader(ctx, ap)
	if err != nil {
		return fmt.Errorf("resolve leader: %w", err)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	title := s.interpolateTemplate(ap, *run, triggerTimezone)
	description := s.buildIssueDescription(ap, *run, triggerTimezone)

	currentAutopilot, err := qtx.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{
		ID:          ap.ID,
		WorkspaceID: ap.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("refresh autopilot: %w", err)
	}
	projectID := currentAutopilot.ProjectID

	if duplicate, found, err := issueguard.LockAndFindRecentAutopilotDuplicate(
		ctx, qtx, ap.WorkspaceID, ap.ID, projectID, title, autopilotRecentDuplicateWindow,
	); err != nil {
		return fmt.Errorf("recent duplicate guard: %w", err)
	} else if found {
		return &errDispatchSkipped{reason: "recent duplicate autopilot issue: " + util.UUIDToString(duplicate.ID), code: dispatch.ReasonAlreadyActive}
	}

	issueNumber, err := qtx.IncrementIssueCounter(ctx, ap.WorkspaceID)
	if err != nil {
		return fmt.Errorf("increment issue counter: %w", err)
	}

	newPosition, err := issueposition.NextTopPosition(ctx, tx, ap.WorkspaceID, "todo")
	if err != nil {
		return fmt.Errorf("get next issue position: %w", err)
	}

	issue, err := qtx.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		WorkspaceID:  ap.WorkspaceID,
		Title:        title,
		Description:  description,
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: ap.AssigneeType, Valid: true},
		AssigneeID:   ap.AssigneeID,

		CreatorType:   "agent",
		CreatorID:     leader.ID,
		ParentIssueID: pgtype.UUID{},
		Position:      newPosition,
		StartDate:     pgtype.Date{},
		DueDate:       pgtype.Date{},
		Number:        issueNumber,
		ProjectID:     projectID,
		OriginType:    pgtype.Text{String: "autopilot", Valid: true},
		OriginID:      ap.ID,
	})
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
	}

	templateSubs, err := qtx.ListAutopilotSubscribers(ctx, ap.ID)
	if err != nil {
		return fmt.Errorf("list autopilot subscribers: %w", err)
	}
	for _, sub := range templateSubs {
		if err := qtx.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID:  issue.ID,
			UserType: sub.UserType,
			UserID:   sub.UserID,
			Reason:   "autopilot",
		}); err != nil {
			return fmt.Errorf("add autopilot subscriber to issue: %w", err)
		}
	}

	updatedRun, err := qtx.UpdateAutopilotRunIssueCreated(ctx, db.UpdateAutopilotRunIssueCreatedParams{
		ID:      run.ID,
		IssueID: issue.ID,
	})
	if err != nil {
		return fmt.Errorf("link run to issue: %w", err)
	}
	*run = updatedRun

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	prefix := s.getIssuePrefix(ap.WorkspaceID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: util.UUIDToString(ap.WorkspaceID),
		ActorType:   "agent",
		ActorID:     util.UUIDToString(leader.ID),
		Payload: map[string]any{
			"issue": issueToMap(issue, prefix),
		},
	})
	s.captureIssueCreatedFromAutopilot(ap, run, issue, leader.ID)

	s.notifyAutopilotSubscribersOnCreate(ctx, ap, issue, leader.ID, templateSubs)

	if ap.AssigneeType == "squad" {

		if !s.autopilotAdmitInvoke(ctx, ap, leader, actorUserID) {
			return fmt.Errorf("not allowed to invoke private squad leader")
		}
		if actorUserID.Valid {
			if _, err := s.TaskSvc.EnqueueTaskForSquadLeaderWithHandoff(ctx, issue, leader.ID, ap.AssigneeID, "", actorUserID); err != nil {
				return fmt.Errorf("enqueue squad leader task: %w", err)
			}
		} else if _, err := s.TaskSvc.EnqueueTaskForSquadLeader(ctx, issue, leader.ID, ap.AssigneeID, pgtype.UUID{}); err != nil {
			return fmt.Errorf("enqueue squad leader task: %w", err)
		}
	} else if actorUserID.Valid {
		if _, err := s.TaskSvc.EnqueueTaskForIssueWithHandoff(ctx, issue, "", actorUserID); err != nil {
			return fmt.Errorf("enqueue task for issue: %w", err)
		}
	} else if _, err := s.TaskSvc.EnqueueTaskForIssue(ctx, issue); err != nil {
		return fmt.Errorf("enqueue task for issue: %w", err)
	}

	slog.Info("autopilot dispatched (create_issue)",
		"autopilot_id", util.UUIDToString(ap.ID),
		"assignee_type", ap.AssigneeType,
		"issue_id", util.UUIDToString(issue.ID),
		"leader_id", util.UUIDToString(leader.ID),
		"run_id", util.UUIDToString(run.ID),
	)
	return nil
}

func (s *AutopilotService) notifyAutopilotSubscribersOnCreate(
	ctx context.Context,
	ap db.Autopilot,
	issue db.Issue,
	leaderID pgtype.UUID,
	subscribers []db.AutopilotSubscriber,
) {
	if len(subscribers) == 0 {
		return
	}
	details, _ := json.Marshal(map[string]string{
		"autopilot_id": util.UUIDToString(ap.ID),
		"reason":       "autopilot",
	})
	for _, sub := range subscribers {

		if sub.UserType != "member" {
			continue
		}
		item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   ap.WorkspaceID,
			RecipientType: "member",
			RecipientID:   sub.UserID,
			Type:          "issue_subscribed",
			Severity:      "info",
			IssueID:       issue.ID,
			Title:         issue.Title,
			Body:          pgtype.Text{},
			ActorType:     pgtype.Text{String: "agent", Valid: true},
			ActorID:       leaderID,
			Details:       details,
		})
		if err != nil {
			slog.Error("autopilot subscriber inbox write failed",
				"autopilot_id", util.UUIDToString(ap.ID),
				"issue_id", util.UUIDToString(issue.ID),
				"recipient_id", util.UUIDToString(sub.UserID),
				"error", err,
			)
			continue
		}
		s.Bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: util.UUIDToString(ap.WorkspaceID),
			ActorType:   "agent",
			ActorID:     util.UUIDToString(leaderID),
			Payload: map[string]any{
				"item": map[string]any{
					"id":             util.UUIDToString(item.ID),
					"workspace_id":   util.UUIDToString(item.WorkspaceID),
					"recipient_type": item.RecipientType,
					"recipient_id":   util.UUIDToString(item.RecipientID),
					"type":           item.Type,
					"severity":       item.Severity,
					"issue_id":       util.UUIDToPtr(item.IssueID),
					"issue_status":   issue.Status,
					"title":          item.Title,
					"body":           util.TextToPtr(item.Body),
					"read":           item.Read,
					"archived":       item.Archived,
					"created_at":     util.TimestampToString(item.CreatedAt),
					"actor_type":     util.TextToPtr(item.ActorType),
					"actor_id":       util.UUIDToPtr(item.ActorID),
					"details":        json.RawMessage(item.Details),
				},
			},
		})
	}
}

type errDispatchSkipped struct {
	reason string

	code dispatch.ReasonCode
}

func (e *errDispatchSkipped) Error() string { return e.reason }

func (s *AutopilotService) dispatchRunOnly(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, actorUserID pgtype.UUID) error {
	agent, _, err := s.resolveAutopilotLeader(ctx, ap)
	if err != nil {

		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errSquadArchived) {
			return &errDispatchSkipped{reason: formatAdmissionReason(ap, "assignee no longer resolvable"), code: dispatch.ReasonTargetUnavailable}
		}
		return fmt.Errorf("resolve leader: %w", err)
	}
	ready, reason, err := AgentReadiness(ctx, s.Queries, agent)
	if err != nil {
		return fmt.Errorf("check agent readiness: %w", err)
	}
	if !ready {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, reason), code: agentReadinessReasonCode(agent)}
	}

	if ap.AssigneeType == "squad" && !s.autopilotAdmitInvoke(ctx, ap, agent, actorUserID) {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, "not allowed to invoke private squad leader"), code: dispatch.ReasonInvocationNotAllowed}
	}

	var autopilotAttr attribution.Result
	if actorUserID.Valid {
		autopilotAttr = attribution.DirectHumanRun(actorUserID, attribution.EvidenceAutopilotRun, run.ID)
	} else {
		autopilotAttr = triggerOwnerAttribution(ctx, s.Queries, run.TriggerID, ap.WorkspaceID, ap.ID, attribution.EvidenceAutopilotRun, run.ID)
	}

	autopilotAttr, err = s.TaskSvc.applyAttributionFallback(ctx, autopilotAttr, agent)
	if err != nil {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, "workspace fail-closed: no accountable human for autopilot run"), code: dispatch.ReasonAttributionBlocked}
	}
	apSource, _, apEvidenceKind, apEvidenceRef := attributionCreateParams(autopilotAttr)

	overlay := s.TaskSvc.buildRuntimeMCPOverlay(ctx, pgtype.UUID{}, agent)

	task, err := s.Queries.CreateAutopilotTask(ctx, db.CreateAutopilotTaskParams{
		AgentID:        agent.ID,
		RuntimeID:      agent.RuntimeID,
		Priority:       0,
		AutopilotRunID: run.ID,

		TriggerSummary: pgtype.Text{
			String: truncateForSummary(ap.Title, triggerSummaryMaxLen),
			Valid:  ap.Title != "",
		},
		OriginatorUserID:     autopilotAttr.UserID,
		AccountableUserID:    autopilotAttr.AccountableUserID,
		RuleVersionID:        autopilotAttr.RuleVersionID,
		OriginatorSource:     apSource,
		TriggerEvidenceKind:  apEvidenceKind,
		TriggerEvidenceRefID: apEvidenceRef,
		RuntimeMcpOverlay:    overlay.Overlay,
		RuntimeConnectedApps: overlay.ConnectedApps,
	})
	if err != nil {
		return fmt.Errorf("create autopilot task: %w", err)
	}

	task = s.TaskSvc.rebindRuntimeMCPOverlayForTask(ctx, s.Queries, task)

	updatedRun, err := s.Queries.UpdateAutopilotRunRunning(ctx, db.UpdateAutopilotRunRunningParams{
		ID:     run.ID,
		TaskID: task.ID,
	})
	if err != nil {
		slog.Warn("failed to update run with task_id", "run_id", util.UUIDToString(run.ID), "error", err)
	} else {
		*run = updatedRun
	}

	s.TaskSvc.NotifyTaskEnqueued(ctx, task)

	slog.Info("autopilot dispatched (run_only)",
		"autopilot_id", util.UUIDToString(ap.ID),
		"task_id", util.UUIDToString(task.ID),
		"run_id", util.UUIDToString(run.ID),
	)
	return nil
}

func (s *AutopilotService) SyncRunFromIssue(ctx context.Context, issue db.Issue) {
	if !issue.OriginType.Valid || issue.OriginType.String != "autopilot" {
		return
	}

	run, err := s.Queries.GetAutopilotRunByIssue(ctx, issue.ID)
	if err != nil {
		return
	}
	autopilot, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		return
	}

	wsID := util.UUIDToString(issue.WorkspaceID)

	switch issue.Status {
	case "done", "in_review":
		updatedRun, err := s.Queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{
			ID: run.ID,
		})
		if err != nil {
			slog.Warn("failed to complete autopilot run", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.captureAutopilotRunCompleted(autopilot, updatedRun)
		s.publishRunDone(wsID, updatedRun, "completed")
	case "cancelled", "blocked":
		reason := "issue " + issue.Status
		updatedRun, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
			ID:            run.ID,
			FailureReason: pgtype.Text{String: reason, Valid: true},
		})
		if err != nil {
			slog.Warn("failed to fail autopilot run", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.captureAutopilotRunFailed(autopilot, updatedRun, updatedRun.Source, reason)
		s.publishRunDone(wsID, updatedRun, "failed")
	}
}

func (s *AutopilotService) SyncRunFromTask(ctx context.Context, task db.AgentTaskQueue) {
	if !task.AutopilotRunID.Valid {
		return
	}

	run, err := s.Queries.GetAutopilotRun(ctx, task.AutopilotRunID)
	if err != nil {
		return
	}

	autopilot, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		return
	}
	wsID := util.UUIDToString(autopilot.WorkspaceID)

	switch task.Status {
	case "completed":
		updatedRun, err := s.Queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{
			ID:     run.ID,
			Result: task.Result,
		})
		if err != nil {
			slog.Warn("failed to complete autopilot run from task", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.captureAutopilotRunCompleted(autopilot, updatedRun)
		s.publishRunDone(wsID, updatedRun, "completed")
	case "failed", "cancelled":
		reason := "task " + task.Status
		if task.Error.Valid {
			reason = task.Error.String
		}
		updatedRun, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
			ID:            run.ID,
			FailureReason: pgtype.Text{String: reason, Valid: true},
		})
		if err != nil {
			slog.Warn("failed to fail autopilot run from task", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.captureAutopilotRunFailed(autopilot, updatedRun, updatedRun.Source, reason)
		s.publishRunDone(wsID, updatedRun, "failed")
	}
}

func (s *AutopilotService) SyncRunFromLinkedIssueTask(ctx context.Context, task db.AgentTaskQueue) {
	if task.AutopilotRunID.Valid || !task.IssueID.Valid || task.Status != "failed" {
		return
	}

	run, err := s.Queries.GetAutopilotRunByIssue(ctx, task.IssueID)
	if err != nil {
		return
	}

	hasActive, err := s.Queries.HasActiveTaskForIssue(ctx, task.IssueID)
	if err != nil {
		slog.Warn("failed to check active tasks for autopilot issue failure",
			"issue_id", util.UUIDToString(task.IssueID),
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
		return
	}
	if hasActive {
		return
	}
	autopilot, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		return
	}

	reason := taskFailureReasonForAutopilotRun(task)
	updatedRun, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
		ID:            run.ID,
		FailureReason: pgtype.Text{String: reason, Valid: reason != ""},
	})
	if err != nil {
		slog.Warn("failed to fail autopilot run from linked issue task",
			"run_id", util.UUIDToString(run.ID),
			"issue_id", util.UUIDToString(task.IssueID),
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
		return
	}
	s.captureAutopilotRunFailed(autopilot, updatedRun, updatedRun.Source, reason)
	s.publishRunDone(util.UUIDToString(autopilot.WorkspaceID), updatedRun, "failed")
}

func taskFailureReasonForAutopilotRun(task db.AgentTaskQueue) string {
	if task.Error.Valid && strings.TrimSpace(task.Error.String) != "" {
		return task.Error.String
	}
	if task.FailureReason.Valid && strings.TrimSpace(task.FailureReason.String) != "" {
		return task.FailureReason.String
	}
	return "task failed"
}

func (s *AutopilotService) handleDispatchSkip(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, err error) (*db.AutopilotRun, dispatch.ReasonCode) {
	var skipErr *errDispatchSkipped
	if !errors.As(err, &skipErr) {
		return nil, ""
	}
	updated, uerr := s.Queries.UpdateAutopilotRunSkipped(ctx, db.UpdateAutopilotRunSkippedParams{
		ID:            run.ID,
		FailureReason: pgtype.Text{String: skipErr.reason, Valid: true},
	})
	if uerr != nil {
		slog.Warn("failed to mark dispatch as skipped",
			"run_id", util.UUIDToString(run.ID), "error", uerr)

		return nil, ""
	}
	*run = updated
	slog.Info("autopilot dispatch skipped post-admission",
		"autopilot_id", util.UUIDToString(ap.ID),
		"run_id", util.UUIDToString(run.ID),
		"reason", skipErr.reason,
	)

	s.Queries.UpdateAutopilotLastRunAt(ctx, ap.ID)
	s.publishRunDone(util.UUIDToString(ap.WorkspaceID), updated, "skipped")
	return run, skipErr.code
}

func (s *AutopilotService) failRun(ctx context.Context, runID pgtype.UUID, reason string) {
	if _, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
		ID:            runID,
		FailureReason: pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		slog.Warn("failed to mark autopilot run as failed", "run_id", util.UUIDToString(runID), "error", err)
	}
}

func (s *AutopilotService) shouldSkipDispatch(ctx context.Context, ap db.Autopilot, actorUserID pgtype.UUID) (string, dispatch.ReasonCode, bool) {
	if !ap.AssigneeID.Valid {
		return "autopilot has no assignee", dispatch.ReasonTargetUnavailable, true
	}
	agent, squadResolved, err := s.resolveAutopilotLeader(ctx, ap)
	if err != nil {

		missing := errors.Is(err, pgx.ErrNoRows)
		archived := errors.Is(err, errSquadArchived)
		slog.Warn("autopilot admission: failed to resolve leader",
			"autopilot_id", util.UUIDToString(ap.ID),
			"assignee_type", ap.AssigneeType,
			"assignee_id", util.UUIDToString(ap.AssigneeID),
			"missing", missing,
			"archived", archived,
			"error", err,
		)
		switch {
		case archived:

			return "assignee squad is archived", dispatch.ReasonTargetUnavailable, true
		case missing && squadResolved:
			return "assignee squad cannot be resolved", dispatch.ReasonTargetUnavailable, true
		case missing && !squadResolved:

			return "assignee agent no longer exists", dispatch.ReasonTargetUnavailable, true
		}

		return "", "", false
	}
	ready, reason, err := AgentReadiness(ctx, s.Queries, agent)
	if err != nil {
		slog.Warn("autopilot admission: failed to load runtime",
			"autopilot_id", util.UUIDToString(ap.ID),
			"runtime_id", util.UUIDToString(agent.RuntimeID),
			"error", err,
		)
		return "", "", false
	}
	if !ready {
		if ap.ExecutionMode == "create_issue" && strings.HasPrefix(reason, "agent runtime is ") {
			slog.Info("autopilot admission: allowing create_issue dispatch for offline runtime",
				"autopilot_id", util.UUIDToString(ap.ID),
				"runtime_id", util.UUIDToString(agent.RuntimeID),
				"reason", reason,
			)
		} else {
			return formatAdmissionReason(ap, reason), agentReadinessReasonCode(agent), true
		}
	}

	if !s.autopilotAdmitInvoke(ctx, ap, agent, actorUserID) {
		if actorUserID.Valid {
			return "you are not allowed to trigger this autopilot's assignee agent", dispatch.ReasonInvocationNotAllowed, true
		}
		return "autopilot creator lacks access to private assignee agent", dispatch.ReasonInvocationNotAllowed, true
	}
	return "", "", false
}

func agentReadinessReasonCode(agent db.Agent) dispatch.ReasonCode {
	if agent.ArchivedAt.Valid {
		return dispatch.ReasonTargetUnavailable
	}
	return dispatch.ReasonRuntimeOffline
}

func formatAdmissionReason(ap db.Autopilot, raw string) string {
	prefix := "assignee "
	if ap.AssigneeType == "squad" {
		prefix = "squad leader "
	}
	switch raw {
	case "agent is archived":
		return prefix + "agent is archived"
	case "agent has no runtime bound":
		return prefix + "agent has no runtime bound"
	default:

		return raw + " at dispatch time"
	}
}

var errSquadArchived = errors.New("squad is archived")

func (s *AutopilotService) resolveAutopilotLeader(ctx context.Context, ap db.Autopilot) (agent db.Agent, squadResolved bool, err error) {
	switch ap.AssigneeType {
	case "", "agent":
		agent, err = s.Queries.GetAgent(ctx, ap.AssigneeID)
		return agent, false, err
	case "squad":
		squad, err := s.Queries.GetSquad(ctx, ap.AssigneeID)
		if err != nil {
			return db.Agent{}, true, fmt.Errorf("load squad: %w", err)
		}
		if squad.ArchivedAt.Valid {
			return db.Agent{}, true, errSquadArchived
		}
		agent, err = s.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil {
			return db.Agent{}, true, fmt.Errorf("load squad leader: %w", err)
		}
		return agent, true, nil
	default:
		return db.Agent{}, false, fmt.Errorf("unknown assignee_type %q", ap.AssigneeType)
	}
}

func autopilotSquadAttribution(ap db.Autopilot) pgtype.UUID {
	if ap.AssigneeType == "squad" && ap.AssigneeID.Valid {
		return ap.AssigneeID
	}
	return pgtype.UUID{}
}

func (s *AutopilotService) recordSkippedRun(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
	plannedAt pgtype.Timestamptz,
	webhookDeliveryID pgtype.UUID,
	reason string,
) (*db.AutopilotRun, error) {
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:       autopilot.ID,
		TriggerID:         triggerID,
		Source:            source,
		Status:            "skipped",
		TriggerPayload:    payload,
		SquadID:           autopilotSquadAttribution(autopilot),
		PlannedAt:         plannedAt,
		WebhookDeliveryID: webhookDeliveryID,
	})
	if err != nil {
		return nil, fmt.Errorf("create skipped run: %w", err)
	}

	updated, err := s.Queries.UpdateAutopilotRunSkipped(ctx, db.UpdateAutopilotRunSkippedParams{
		ID:            run.ID,
		FailureReason: pgtype.Text{String: reason, Valid: true},
	})
	if err == nil {
		run = updated
	} else {
		slog.Warn("failed to set skip reason on autopilot run",
			"run_id", util.UUIDToString(run.ID), "error", err)
	}

	slog.Info("autopilot dispatch skipped",
		"autopilot_id", util.UUIDToString(autopilot.ID),
		"run_id", util.UUIDToString(run.ID),
		"source", source,
		"reason", reason,
	)

	s.Queries.UpdateAutopilotLastRunAt(ctx, autopilot.ID)

	s.publishRunDone(util.UUIDToString(autopilot.WorkspaceID), run, "skipped")
	return &run, nil
}

func (s *AutopilotService) publishRunDone(workspaceID string, run db.AutopilotRun, status string) {
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAutopilotRunDone,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"run_id":       util.UUIDToString(run.ID),
			"autopilot_id": util.UUIDToString(run.AutopilotID),
			"status":       status,
		},
	})
}

func (s *AutopilotService) captureIssueCreatedFromAutopilot(ap db.Autopilot, run *db.AutopilotRun, issue db.Issue, leaderID pgtype.UUID) {
	if s.TaskSvc == nil || s.TaskSvc.Analytics == nil {
		return
	}

	obsmetrics.RecordEvent(s.TaskSvc.Analytics, s.TaskSvc.Metrics, analytics.IssueCreated(
		autopilotActorID(ap),
		util.UUIDToString(ap.WorkspaceID),
		util.UUIDToString(issue.ID),
		util.UUIDToString(leaderID),
		"",
		util.UUIDToString(run.ID),
		analytics.SourceAutopilot,
		analytics.PlatformServer,
	))
}

func (s *AutopilotService) captureAutopilotRunStarted(ap db.Autopilot, run db.AutopilotRun, triggerSource string) {
	if s.TaskSvc == nil || s.TaskSvc.Analytics == nil {
		return
	}
	obsmetrics.RecordEvent(s.TaskSvc.Analytics, s.TaskSvc.Metrics, analytics.AutopilotRunStarted(
		autopilotActorID(ap),
		util.UUIDToString(ap.WorkspaceID),
		util.UUIDToString(ap.ID),
		util.UUIDToString(run.ID),
		triggerSource,
		s.autopilotAssigneeAnalytics(ap),
		triggerSource,
	))
}

func (s *AutopilotService) captureAutopilotRunCompleted(ap db.Autopilot, run db.AutopilotRun) {
	if s.TaskSvc == nil || s.TaskSvc.Analytics == nil {
		return
	}
	obsmetrics.RecordEvent(s.TaskSvc.Analytics, s.TaskSvc.Metrics, analytics.AutopilotRunCompleted(
		autopilotActorID(ap),
		util.UUIDToString(ap.WorkspaceID),
		util.UUIDToString(ap.ID),
		util.UUIDToString(run.ID),
		run.Source,
		s.autopilotAssigneeAnalytics(ap),
		run.Source,
		autopilotRunDurationMS(run),
	))
}

func (s *AutopilotService) captureAutopilotRunFailed(ap db.Autopilot, run db.AutopilotRun, triggerSource, reason string) {
	if s.TaskSvc == nil || s.TaskSvc.Analytics == nil {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	obsmetrics.RecordEvent(s.TaskSvc.Analytics, s.TaskSvc.Metrics, analytics.AutopilotRunFailed(
		autopilotActorID(ap),
		util.UUIDToString(ap.WorkspaceID),
		util.UUIDToString(ap.ID),
		util.UUIDToString(run.ID),
		triggerSource,
		s.autopilotAssigneeAnalytics(ap),
		triggerSource,
		reason,
		autopilotErrorType(reason),
		false,
		autopilotRunDurationMS(run),
	))
}

func (s *AutopilotService) autopilotAssigneeAnalytics(ap db.Autopilot) analytics.AutopilotAssignee {
	assignee := analytics.AutopilotAssignee{
		AssigneeType: ap.AssigneeType,
	}
	if ap.AssigneeType == "squad" {
		assignee.SquadID = util.UUIDToString(ap.AssigneeID)
		if leader, _, err := s.resolveAutopilotLeader(context.Background(), ap); err == nil {
			assignee.AgentID = util.UUIDToString(leader.ID)
		} else {
			assignee.AgentID = util.UUIDToString(ap.AssigneeID)
		}
	} else {
		assignee.AgentID = util.UUIDToString(ap.AssigneeID)
	}
	return assignee
}

func autopilotErrorType(reason string) string {
	switch {
	case strings.Contains(reason, "unknown execution_mode"):
		return "configuration"
	case strings.HasPrefix(reason, "issue "):
		return "issue_terminal"
	case strings.Contains(reason, "create issue"), strings.Contains(reason, "enqueue task"), strings.Contains(reason, "dispatch"):
		return "dispatch_error"
	case strings.HasPrefix(reason, "task "):
		return "task_error"
	default:
		return "autopilot_error"
	}
}

func autopilotActorID(ap db.Autopilot) string {
	id := util.UUIDToString(ap.CreatedByID)
	if ap.CreatedByType == "agent" && id != "" {
		return "agent:" + id
	}
	if id != "" {
		return id
	}
	return "system"
}

func autopilotRunDurationMS(run db.AutopilotRun) int64 {
	if !run.CompletedAt.Valid {
		return 0
	}
	start := run.TriggeredAt
	if !start.Valid {
		start = run.CreatedAt
	}
	if !start.Valid {
		return 0
	}
	ms := run.CompletedAt.Time.Sub(start.Time).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func (s *AutopilotService) resolveAutopilotTriggerTimezone(ctx context.Context, triggerID pgtype.UUID) string {
	if !triggerID.Valid || s == nil || s.Queries == nil {
		return DefaultAutopilotTriggerTimezone
	}

	trigger, err := s.Queries.GetAutopilotTrigger(ctx, triggerID)
	if err != nil {
		slog.Warn("failed to load autopilot trigger timezone; falling back to UTC",
			"trigger_id", util.UUIDToString(triggerID),
			"error", err,
		)
		return DefaultAutopilotTriggerTimezone
	}

	timezone := strings.TrimSpace(trigger.Timezone.String)
	if !trigger.Timezone.Valid || timezone == "" {
		return DefaultAutopilotTriggerTimezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		slog.Warn("invalid autopilot trigger timezone; falling back to UTC",
			"trigger_id", util.UUIDToString(triggerID),
			"timezone", timezone,
			"error", err,
		)
		return DefaultAutopilotTriggerTimezone
	}
	return timezone
}

func formatAutopilotRunTimestamp(run db.AutopilotRun, timezone string) string {
	triggeredAt := autopilotRunTriggeredAt(run)
	loc, label := autopilotTriggerLocation(timezone)
	return triggeredAt.In(loc).Format("2006-01-02 15:04") + " " + label
}

func formatAutopilotRunDate(run db.AutopilotRun, timezone string) string {
	triggeredAt := autopilotRunTriggeredAt(run)
	loc, _ := autopilotTriggerLocation(timezone)
	return triggeredAt.In(loc).Format("2006-01-02")
}

func autopilotRunTriggeredAt(run db.AutopilotRun) time.Time {
	if run.TriggeredAt.Valid {
		return run.TriggeredAt.Time
	}
	if run.CreatedAt.Valid {
		return run.CreatedAt.Time
	}
	return time.Now().UTC()
}

func autopilotTriggerLocation(timezone string) (*time.Location, string) {
	label := strings.TrimSpace(timezone)
	if label == "" {
		label = DefaultAutopilotTriggerTimezone
	}
	loc, err := time.LoadLocation(label)
	if err != nil {
		return time.UTC, DefaultAutopilotTriggerTimezone
	}
	return loc, label
}

func (s *AutopilotService) buildIssueDescription(ap db.Autopilot, run db.AutopilotRun, triggerTimezone string) pgtype.Text {
	triggeredAt := formatAutopilotRunTimestamp(run, triggerTimezone)
	var b strings.Builder
	b.WriteString(ap.Description.String)
	b.WriteString("\n\n---\n*Autopilot run triggered at ")
	b.WriteString(triggeredAt)
	b.WriteString(". After starting work, rename this issue to accurately reflect what you are doing.*")

	if run.Source == "webhook" && len(run.TriggerPayload) > 0 {
		event := "webhook.received"
		var payloadJSON []byte
		var env struct {
			Event        string          `json:"event"`
			EventPayload json.RawMessage `json:"eventPayload"`
		}
		if err := json.Unmarshal(run.TriggerPayload, &env); err == nil {
			if env.Event != "" {
				event = env.Event
			}
			if len(env.EventPayload) > 0 {
				if pretty, err := prettifyJSON(env.EventPayload); err == nil {
					payloadJSON = pretty
				}
			}
		}
		if len(payloadJSON) == 0 {
			if pretty, err := prettifyJSON(run.TriggerPayload); err == nil {
				payloadJSON = pretty
			} else {
				payloadJSON = run.TriggerPayload
			}
		}
		b.WriteString("\n\nWebhook event: ")
		b.WriteString(event)
		b.WriteString("\n\nWebhook payload:\n```json\n")
		b.Write(payloadJSON)
		b.WriteString("\n```")
	}

	return pgtype.Text{String: b.String(), Valid: true}
}

func prettifyJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}

var issueTitleTemplateTokenRE = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

func (s *AutopilotService) interpolateTemplate(ap db.Autopilot, run db.AutopilotRun, triggerTimezone string) string {
	tmpl := ap.Title
	if ap.IssueTitleTemplate.Valid && ap.IssueTitleTemplate.String != "" {
		tmpl = ap.IssueTitleTemplate.String
	}
	triggerDate := formatAutopilotRunDate(run, triggerTimezone)
	return issueTitleTemplateTokenRE.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := strings.TrimSpace(match[2 : len(match)-2])
		switch name {
		case "date":
			return triggerDate
		default:
			return match
		}
	})
}

var SupportedIssueTitleTemplateVariables = []string{"date"}

func ValidateIssueTitleTemplate(tmpl string) error {
	if tmpl == "" {
		return nil
	}
	for _, m := range issueTitleTemplateTokenRE.FindAllStringSubmatch(tmpl, -1) {
		name := m[1]
		if !isSupportedIssueTitleVariable(name) {
			return fmt.Errorf(
				"unknown template variable %q; supported: {{%s}}",
				name,
				strings.Join(SupportedIssueTitleTemplateVariables, "}}, {{"),
			)
		}
	}
	return nil
}

func isSupportedIssueTitleVariable(name string) bool {
	for _, v := range SupportedIssueTitleTemplateVariables {
		if name == v {
			return true
		}
	}
	return false
}

func (s *AutopilotService) getIssuePrefix(workspaceID pgtype.UUID) string {
	ws, err := s.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}

func (s *AutopilotService) autopilotAdmitInvoke(ctx context.Context, ap db.Autopilot, agent db.Agent, actorUserID pgtype.UUID) bool {
	if actorUserID.Valid {
		return s.canMemberInvokeAgent(ctx, agent, actorUserID, ap.WorkspaceID)
	}
	return s.canCreatorInvokeAgent(ctx, ap, agent)
}

func (s *AutopilotService) canMemberInvokeAgent(ctx context.Context, agent db.Agent, memberUserID pgtype.UUID, workspaceID pgtype.UUID) bool {
	userID := util.UUIDToString(memberUserID)
	if userID == "" {
		return false
	}
	if util.UUIDToString(agent.OwnerID) == userID {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	targets, err := s.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	isWorkspaceMember := false
	if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      memberUserID,
		WorkspaceID: workspaceID,
	}); err == nil {
		isWorkspaceMember = true
	}
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember {
				return true
			}
		case "member":
			if util.UUIDToString(t.TargetID) == userID {
				return true
			}
		}
	}
	return false
}

func (s *AutopilotService) canCreatorInvokeAgent(ctx context.Context, ap db.Autopilot, agent db.Agent) bool {
	creatorID := util.UUIDToString(ap.CreatedByID)
	if ap.CreatedByType == "member" && util.UUIDToString(agent.OwnerID) == creatorID {
		return true
	}
	if agent.PermissionMode != "public_to" {

		return false
	}
	targets, err := s.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}

	workspaceBroad := ap.CreatedByType == "agent"
	isWorkspaceMember := false
	if ap.CreatedByType == "member" {
		if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      ap.CreatedByID,
			WorkspaceID: ap.WorkspaceID,
		}); err == nil {
			isWorkspaceMember = true
		}
	}
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember || workspaceBroad {
				return true
			}
		case "member":
			if ap.CreatedByType == "member" && util.UUIDToString(t.TargetID) == creatorID {
				return true
			}
		}
	}
	return false
}
