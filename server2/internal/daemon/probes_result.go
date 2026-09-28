// probes_result.go — daemonReport{Update,ModelList,LocalSkills,LocalSkillImport}Result:
// демон отчитывается о результате заявки, созданной internal/runtime
// (initiateRuntime*). Все четыре ручки контракта всегда отвечают
// `200 {"status":"ok"}`, даже когда сама заявка уже терминальна/неизвестна
// (contract §1.2) — поэтому здесь нет отдельной ветки "заявка не найдена"
// с иным кодом ответа, только внутреннее found, которое ни на что не влияет
// кроме журнала.
package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// reportBody — общая часть тела всех четырёх daemonReport*Result: status +
// error, плюс произвольный "хвост", специфичный конкретному виду заявки.
type reportBody struct {
	Status string          `json:"status"`
	Error  string          `json:"error"`
	Raw    json.RawMessage `json:"-"`
}

func decodeReportBody(r *http.Request, allowed ...string) (reportBody, bool) {
	raw, err := httpapi.ReadBody(r)
	if err != nil {
		return reportBody{}, false
	}
	var body reportBody
	if err := json.Unmarshal(raw, &body); err != nil || !isOneOf(body.Status, allowed...) {
		return reportBody{}, false
	}
	body.Raw = raw
	return body, true
}

func isOneOf(v string, options ...string) bool {
	for _, o := range options {
		if v == o {
			return true
		}
	}
	return false
}

// saveProbeOutcome — общий низ всех четырёх ручек: резолвит рантайм,
// разбирает тело, сохраняет исход (некритичная ошибка сохранения только
// логируется — контракт требует 200 в любом случае), логирует под своей
// меткой. after, если задан, выполняется только при status=completed
// (публикация skill:created для импорта навыка).
func (d *Deps) saveProbeOutcome(w http.ResponseWriter, r *http.Request, logLabel string, allowedStatus []string,
	outcome func(reportBody) any, after func(exWorkspaceID string, body reportBody)) {
	_, ex, ok := d.requireRuntimeAccess(w, r)
	if !ok {
		return
	}
	body, ok := decodeReportBody(r, allowedStatus...)
	if !ok {
		httpapi.BadRequest(w, "invalid body")
		return
	}
	if _, _, err := d.Runtime.Store.ReportResult(r.Context(), ex.ID, r.PathValue(probeIDParam(r)), body.Status, outcome(body), body.Error); err != nil {
		d.Logger.Warn("daemon: не удалось сохранить результат заявки", "kind", logLabel, "err", err)
	}
	if after != nil {
		after(ex.WorkspaceID, body)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// probeIDParam — маршруты этого файла называют {id} по-разному: updateId
// или requestId (см. register.go).
func probeIDParam(r *http.Request) string {
	if v := r.PathValue("updateId"); v != "" {
		return "updateId"
	}
	return "requestId"
}

func (d *Deps) handleReportUpdateResult(w http.ResponseWriter, r *http.Request) {
	d.saveProbeOutcome(w, r, "self_update", []string{"running", "completed", "failed"}, func(body reportBody) any {
		var payload struct {
			Output string `json:"output"`
		}
		_ = json.Unmarshal(body.Raw, &payload)
		return map[string]string{"output": payload.Output}
	}, nil)
}

func (d *Deps) handleReportModelListResult(w http.ResponseWriter, r *http.Request) {
	d.saveProbeOutcome(w, r, "model_list", []string{"completed", "failed"}, func(body reportBody) any {
		var payload struct {
			Models    any   `json:"models"`
			Supported *bool `json:"supported"`
		}
		_ = json.Unmarshal(body.Raw, &payload)
		return map[string]any{"models": arrayOrEmpty(payload.Models), "supported": boolOr(payload.Supported, true)}
	}, nil)
}

func (d *Deps) handleReportLocalSkillsResult(w http.ResponseWriter, r *http.Request) {
	d.saveProbeOutcome(w, r, "local_skills", []string{"completed", "failed"}, func(body reportBody) any {
		var payload struct {
			Skills       any   `json:"skills"`
			Supported    *bool `json:"supported"`
			McpServers   any   `json:"mcp_servers"`
			McpSupported *bool `json:"mcp_supported"`
		}
		_ = json.Unmarshal(body.Raw, &payload)
		return map[string]any{
			"skills": arrayOrEmpty(payload.Skills), "supported": boolOr(payload.Supported, false),
			"mcp_servers": arrayOrEmpty(payload.McpServers), "mcp_supported": boolOr(payload.McpSupported, false),
		}
	}, nil)
}

// arrayOrEmpty — contract: skills/models/mcp_servers в
// RuntimeLocalSkillListRequest/RuntimeModelListRequest не nullable (всегда
// массив); daemon вправе не прислать ключ вовсе (например при
// status=failed), а декодирование отсутствующего JSON-ключа в `any` даёт Go
// nil, который json.Marshal превращает в literal null, а не "[]" — T-029
// доводка.
func arrayOrEmpty(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func (d *Deps) handleReportLocalSkillImportResult(w http.ResponseWriter, r *http.Request) {
	_, ex, ok := d.requireRuntimeAccess(w, r)
	if !ok {
		return
	}
	body, ok := decodeReportBody(r, "completed", "failed")
	if !ok {
		httpapi.BadRequest(w, "invalid body")
		return
	}
	var payload struct {
		Skill any `json:"skill"`
	}
	_ = json.Unmarshal(body.Raw, &payload)
	if body.Status == "completed" && payload.Skill == nil {
		httpapi.BadRequest(w, "completed without a skill payload")
		return
	}
	if _, _, err := d.Runtime.Store.ReportResult(r.Context(), ex.ID, r.PathValue("requestId"), body.Status,
		map[string]any{"skill": payload.Skill}, body.Error); err != nil {
		d.Logger.Warn("daemon: не удалось сохранить результат импорта навыка", "err", err)
	}
	if body.Status == "completed" {
		d.publish(ex.WorkspaceID, "skill:created", map[string]any{"skill": payload.Skill})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func boolOr(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}
