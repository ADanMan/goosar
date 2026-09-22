// Пакет agenttmpl загружает и отдаёт встроенные шаблоны агентов для сценария
// «Создать агента из шаблона». Шаблоны — статические JSON, встроенные при сборке:
// блок инструкций плюс список ссылок на skills, материализуемых в воркспейс.
package agenttmpl

type Template struct {
	Slug string `json:"slug"`

	Name string `json:"name"`

	Description string `json:"description"`

	Category string `json:"category,omitempty"`

	Icon string `json:"icon,omitempty"`

	Accent string `json:"accent,omitempty"`

	Instructions string `json:"instructions"`

	Skills []TemplateSkillRef `json:"skills"`
}

type TemplateSkillRef struct {
	SourceURL string `json:"source_url"`

	CachedName string `json:"cached_name"`

	CachedDescription string `json:"cached_description"`
}
