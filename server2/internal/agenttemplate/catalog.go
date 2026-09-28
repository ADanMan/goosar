// Package agenttemplate реализует `/api/agent-templates` (contract §1
// "Шаблоны агентов, конструктор агента..."): каталог шаблонов агентов,
// зашитый в поставку сервера (не в БД воркспейса), доступный только на
// чтение. Реальный набор шаблонов контракт/data-model не перечисляют (только
// форму AgentTemplateSummary/AgentTemplate) — содержимое ниже придумано этой
// сессией как разумный минимальный каталог (см. server2/docs/decisions.md,
// раздел T-028): общий помощник и два ролевых агента без внешних навыков
// (skills: []), чтобы `POST /api/agents/from-template` не зависело от
// сетевого источника по умолчанию.
package agenttemplate

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// SkillRef — components/schemas/AgentTemplateSkillRef.
type SkillRef struct {
	SourceURL         string `json:"source_url"`
	CachedName        string `json:"cached_name"`
	CachedDescription string `json:"cached_description"`
}

// Template — components/schemas/AgentTemplate (allOf Summary + instructions).
type Template struct {
	Slug         string     `json:"slug"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Category     string     `json:"category"`
	Icon         string     `json:"icon"`
	Accent       string     `json:"accent"`
	Skills       []SkillRef `json:"skills"`
	Instructions string     `json:"instructions"`
}

// Summary — components/schemas/AgentTemplateSummary (без instructions).
func (t Template) Summary() map[string]any {
	return map[string]any{
		"slug":        t.Slug,
		"name":        t.Name,
		"description": t.Description,
		"category":    t.Category,
		"icon":        t.Icon,
		"accent":      t.Accent,
		"skills":      skillsOrEmpty(t.Skills),
	}
}

func skillsOrEmpty(s []SkillRef) []SkillRef {
	if s == nil {
		return []SkillRef{}
	}
	return s
}

// Full — форма AgentTemplate целиком, включая instructions.
func (t Template) Full() map[string]any {
	out := t.Summary()
	out["instructions"] = t.Instructions
	return out
}

var catalog = []Template{
	{
		Slug:        "general-assistant",
		Name:        "Универсальный помощник",
		Description: "Общего назначения агент для повседневных задач воркспейса.",
		Category:    "general",
		Icon:        "sparkles",
		Accent:      "#6366F1",
		Skills:      []SkillRef{},
		Instructions: "Ты — помощник команды в системе Goosar. Отвечай по существу, " +
			"уточняй задачу, если формулировка неоднозначна, и предлагай следующий шаг.",
	},
	{
		Slug:        "code-reviewer",
		Name:        "Ревьюер кода",
		Description: "Проверяет pull request'ы на корректность, стиль и риски регрессии.",
		Category:    "engineering",
		Icon:        "code",
		Accent:      "#22C55E",
		Skills:      []SkillRef{},
		Instructions: "Ты — ревьюер кода. Для каждой задачи находи конкретные ошибки и риски, " +
			"предлагай минимальные точные правки, не переписывай код целиком без необходимости.",
	},
	{
		Slug:        "researcher",
		Name:        "Исследователь",
		Description: "Собирает и суммирует информацию по задаче из доступных источников воркспейса.",
		Category:    "research",
		Icon:        "search",
		Accent:      "#F59E0B",
		Skills:      []SkillRef{},
		Instructions: "Ты — исследователь. Собирай факты, указывай источники, отделяй " +
			"проверенные утверждения от предположений.",
	},
}

// List — GET /api/agent-templates (сводки, без instructions).
func List() []Template {
	out := make([]Template, len(catalog))
	copy(out, catalog)
	return out
}

// Get — GET /api/agent-templates/{slug}. ok=false — слаг не зарегистрирован (404).
func Get(slug string) (Template, bool) {
	for _, t := range catalog {
		if t.Slug == slug {
			return t, true
		}
	}
	return Template{}, false
}

// --- HTTP-слой ---------------------------------------------------------------
//
// Домен не хранит состояние (каталог выше зашит в бинарник), кроме резолва
// воркспейса из заголовка (contract: x-roles owner/admin/member — нужно
// только членство, не сама сущность воркспейса).

// Deps — зависимости домена agenttemplate.
type Deps struct {
	Workspace httpapi.WorkspaceMembership
}

func New(ws httpapi.WorkspaceMembership) *Deps { return &Deps{Workspace: ws} }

// Register регистрирует `/api/agent-templates` (contract §1 раздела
// "Шаблоны агентов...").
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/agent-templates", deps.handleList)
	router.Handle(http.MethodGet, "/api/agent-templates/{slug}", deps.handleGet)
}

// handleList отвечает сводками каталога (listAgentTemplates): вызывающему
// достаточно членства в воркспейсе — сам каталог одинаков для всех.
func (d *Deps) handleList(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace); !ok {
		return
	}
	list := List()
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		out = append(out, t.Summary())
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// handleGet отвечает полным шаблоном по slug (getAgentTemplate), включая
// instructions — единственное поле, которого нет в сводке handleList.
func (d *Deps) handleGet(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace); !ok {
		return
	}
	t, ok := Get(r.PathValue("slug"))
	if !ok {
		httpapi.NotFound(w, "template not found")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, t.Full())
}
