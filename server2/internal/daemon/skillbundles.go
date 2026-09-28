// skillbundles.go — POST .../tasks/{taskId}/skill-bundles/resolve. Этот
// сервер всегда отдаёт агенту полное содержимое навыков в AgentTask.agent.skills
// (skill_refs не используется — см. decisions.md), поэтому референсный
// daemon-клиент этот маршрут при обычной работе не вызывает; реализован
// структурно для полноты контракта и на случай явного запроса содержимого
// по id (например при повторной валидации кэша).
package daemon

import (
	"encoding/base64"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

type skillRef struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Hash   string `json:"hash"`
}

func (ref skillRef) valid() bool { return ref.ID != "" && ref.Source != "" && ref.Hash != "" }

type resolveSkillsRequest struct {
	Skills          []skillRef `json:"skills"`
	ContentEncoding string     `json:"content_encoding"`
}

func (d *Deps) handleResolveSkillBundles(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := d.requireRuntimeAccess(w, r); !ok {
		return
	}
	if !d.taskAcceptsSkillResolution(w, r) {
		return
	}
	var req resolveSkillsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	bundles, resolveErr := d.resolveSkillRefs(r, req.Skills, req.ContentEncoding == "base64")
	if resolveErr != "" {
		httpapi.NotFound(w, resolveErr)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"bundles": bundles, "content_encoding": req.ContentEncoding})
}

// taskAcceptsSkillResolution — задача существует у этого рантайма и ещё
// готовится (dispatched/waiting_local_directory); сама пишет 404/409.
func (d *Deps) taskAcceptsSkillResolution(w http.ResponseWriter, r *http.Request) bool {
	status, found, err := d.Dispatch.Store.StatusOf(r.Context(), d.DB.Pool, r.PathValue("taskId"))
	switch {
	case err != nil:
		d.internalErr(w, err)
	case !found:
		httpapi.NotFound(w, "task not found for this runtime")
	case status == "dispatched" || status == "waiting_local_directory":
		return true
	default:
		httpapi.WriteError(w, http.StatusConflict, "task is not in a state that allows resolving skills", "")
	}
	return false
}

// resolveSkillRefs резолвит каждую ссылку в полный AgentSkillBundle; пустое
// сообщение об ошибке — все ссылки разрешены успешно.
func (d *Deps) resolveSkillRefs(r *http.Request, refs []skillRef, base64Content bool) ([]map[string]any, string) {
	bundles := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		if !ref.valid() {
			return nil, "each skill ref requires id, source and hash"
		}
		bundle, ok := d.skillBundleFor(r, ref, base64Content)
		if !ok {
			return nil, "referenced skill bundle is not allowed for this agent"
		}
		bundles = append(bundles, bundle)
	}
	return bundles, ""
}

func (d *Deps) skillBundleFor(r *http.Request, ref skillRef, base64Content bool) (map[string]any, bool) {
	var title, body string
	var summary *string
	err := d.DB.Pool.QueryRow(r.Context(), `SELECT cap_title, cap_summary, cap_body_md FROM capabilities WHERE id = $1`, ref.ID).
		Scan(&title, &summary, &body)
	if err != nil {
		return nil, false
	}
	content := body
	if base64Content {
		content = base64.StdEncoding.EncodeToString([]byte(body))
	}
	bundle := map[string]any{
		"id": ref.ID, "source": ref.Source, "name": title, "hash": ref.Hash, "size_bytes": len(body), "content": content,
	}
	if summary != nil {
		bundle["description"] = *summary
	}
	return bundle, true
}
