// dispatcher.go — общая точка допуска (admission) для всех трёх источников
// срабатывания: расписание (scheduler.go), ручной запуск
// (handleTriggerAutopilot) и вебхук (handlers_webhook.go). Contract §7.3:
// Run — факт срабатывания (создание задачи и/или запуск исполнителя).
package autopilot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/task"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Dispatcher связывает autopilot.Store с dispatch.Deps (постановка агента в
// очередь) и task.Store (создание задачи в режиме create_issue). DB — тот же
// пул, что и Store (dispatch.Enqueue принимает Querier — здесь это
// DB.Pool, ровно как это делает internal/note.dispatchAdapter).
type Dispatcher struct {
	Store     *Store
	Dispatch  *dispatch.Deps
	Tasks     *task.Store
	Workspace *workspace.Store
	DB        *store.Store
}

// AdmitResult — то, что нужно вызывающему (ручной триггер/вебхук) после
// admission: сам Run плюс признак "run вообще создан" (веб-хук admission
// может решить не создавать Run вовсе — ignored/rejected, contract §7.3).
type AdmitResult struct {
	Run     Run
	Created bool
}

// admit — общая логика допуска: sentinel должен быть active, исполнитель —
// существовать/не быть архивированным; при этих условиях создаёт Run и, по
// execution_mode, либо создаёт задачу (create_issue) + ставит агента в
// очередь, либо только ставит агента в очередь (run_only). Любое отклонение
// — Run со статусом skipped и reason_code, не ошибка Go (contract:
// "Созданный прогон (может быть сразу skipped с reason_code...)").
func (d *Dispatcher) admit(ctx context.Context, sentinelID string, triggerID *string, source string, payload json.RawMessage) (AdmitResult, error) {
	raw, err := d.Store.GetRaw(ctx, sentinelID)
	if err != nil {
		return AdmitResult{}, err
	}
	if raw.Status != "active" {
		run, err := d.Store.CreateRun(ctx, CreateRunParams{
			AutopilotID: sentinelID, TriggerID: triggerID, Source: source, Status: "skipped",
			ReasonCode: strPtr("autopilot_not_active"), TriggerPayload: payload,
		})
		return AdmitResult{Run: run, Created: true}, err
	}

	assignee, err := d.Store.ResolveAssignee(ctx, raw.WorkspaceID, raw.AssigneeType, raw.AssigneeID)
	if err != nil {
		reason := "assignee_unavailable"
		switch {
		case err == ErrAssigneeNotFound:
			reason = "assignee_not_found"
		case err == ErrSquadLeaderNotAgent:
			reason = "squad_leader_not_agent"
		}
		run, cerr := d.Store.CreateRun(ctx, CreateRunParams{
			AutopilotID: sentinelID, TriggerID: triggerID, Source: source, Status: "skipped",
			ReasonCode: &reason, TriggerPayload: payload,
		})
		return AdmitResult{Run: run, Created: true}, cerr
	}
	if assignee.Archived {
		run, cerr := d.Store.CreateRun(ctx, CreateRunParams{
			AutopilotID: sentinelID, TriggerID: triggerID, Source: source, Status: "skipped",
			ReasonCode: strPtr("assignee_archived"), TriggerPayload: payload,
		})
		return AdmitResult{Run: run, Created: true}, cerr
	}

	var issueID *string
	status := "running"
	if raw.ExecutionMode == "create_issue" {
		title := issueTitleFor(raw, source)
		issue, err := d.Tasks.CreateIssue(ctx, d.Workspace, task.CreateParams{
			WorkspaceID: raw.WorkspaceID, Title: title, ProjectID: raw.ProjectID,
			CreatorType: raw.CreatedByType, CreatorID: raw.CreatedByID,
		})
		if err != nil {
			run, cerr := d.Store.CreateRun(ctx, CreateRunParams{
				AutopilotID: sentinelID, TriggerID: triggerID, Source: source, Status: "failed",
				FailureReason: strPtr(err.Error()), TriggerPayload: payload,
			})
			if cerr != nil {
				return AdmitResult{}, cerr
			}
			return AdmitResult{Run: run, Created: true}, nil
		}
		issueID = &issue.ID
		status = "issue_created"
	}

	run, err := d.Store.CreateRun(ctx, CreateRunParams{
		AutopilotID: sentinelID, TriggerID: triggerID, Source: source, Status: status,
		IssueID: issueID, TriggerPayload: payload,
	})
	if err != nil {
		return AdmitResult{}, err
	}

	jobSpec := dispatch.JobSpec{
		WorkspaceID: raw.WorkspaceID, OperativeID: assignee.OperativeID, ExecutorID: assignee.ExecutorID,
		CrewID: assignee.CrewID, Kind: dispatch.KindAutopilot, SentinelRunID: run.ID,
		InitiatorType: "system", ThreadTitle: raw.Title,
	}
	if issueID != nil {
		jobSpec.TicketID = *issueID
	}
	if _, err := d.Dispatch.Enqueue(ctx, d.DB.Pool, jobSpec); err != nil {
		reason := err.Error()
		_ = d.Store.CompleteRun(ctx, run.ID, "failed", timePtr(time.Now().UTC()))
		run.Status = "failed"
		run.FailureReason = &reason
		return AdmitResult{Run: run, Created: true}, nil
	}

	if err := d.Store.SetLastRunAt(ctx, sentinelID, time.Now().UTC()); err != nil {
		return AdmitResult{}, err
	}
	return AdmitResult{Run: run, Created: true}, nil
}

func issueTitleFor(raw Raw, source string) string {
	if raw.IssueTitleTemplate != nil && *raw.IssueTitleTemplate != "" {
		return renderTemplate(*raw.IssueTitleTemplate, raw, source)
	}
	return fmt.Sprintf("%s — %s", raw.Title, time.Now().UTC().Format("2006-01-02 15:04"))
}

// renderTemplate — подстановка минимального набора плейсхолдеров в
// issue_title_template (контракт не формализует синтаксис шаблона — решение
// зафиксировано в server2/docs/decisions.md, раздел T-028): `{title}` —
// заголовок автопилота, `{date}` — дата запуска (YYYY-MM-DD), `{source}` —
// schedule/manual/webhook/api.
func renderTemplate(tmpl string, raw Raw, source string) string {
	out := tmpl
	out = replaceAll(out, "{title}", raw.Title)
	out = replaceAll(out, "{date}", time.Now().UTC().Format("2006-01-02"))
	out = replaceAll(out, "{source}", source)
	return out
}

func replaceAll(s, old, repl string) string {
	for {
		idx := indexOf(s, old)
		if idx < 0 {
			return s
		}
		s = s[:idx] + repl + s[idx+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }

// DispatchManual — triggerAutopilot (source=manual, без trigger_id: ручной
// запуск не привязан ни к одному конкретному триггеру).
func (d *Dispatcher) DispatchManual(ctx context.Context, sentinelID string) (AdmitResult, error) {
	return d.admit(ctx, sentinelID, nil, "manual", nil)
}

// DispatchSchedule — вызывается scheduler.go при срабатывании cron-триггера.
func (d *Dispatcher) DispatchSchedule(ctx context.Context, sentinelID, triggerID string) error {
	tid := triggerID
	_, err := d.admit(ctx, sentinelID, &tid, "schedule", nil)
	return err
}

// DispatchWebhook — вызывается handlers_webhook.go при принятой (admitted)
// доставке; payload — нормализованный конверт `{event, eventPayload, request}`.
func (d *Dispatcher) DispatchWebhook(ctx context.Context, sentinelID, triggerID string, payload json.RawMessage) (AdmitResult, error) {
	tid := triggerID
	return d.admit(ctx, sentinelID, &tid, "webhook", payload)
}
