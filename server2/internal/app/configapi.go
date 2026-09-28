package app

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// RegisterConfig регистрирует GET /api/config (components/schemas/AppConfig).
func RegisterConfig(router *httpapi.Router, d *Deps) {
	router.Handle(http.MethodGet, "/api/config", d.handleGetPublicConfig)
}

// publicConfig строит components/schemas/AppConfig для этого деплоя. Поля,
// которые контракт документирует, но T-026 ещё не может честно заполнить
// (интеграции, облачный рантайм — T-029), отдаются в их "выключенном"
// значении, а не опускаются — это валидная и предсказуемая форма ответа.
func (d *Deps) publicConfig() map[string]any {
	return map[string]any{
		"allow_signup":                d.Config.AllowSignup,
		"workspace_creation_disabled": false,
		"server_version":              d.Config.ServerVersion,
		"delivery_profile":            "self-hosted",
		"vcs_integration_available":   false,
		"email_transport":             "logger",
		"external_images":             "block",
		"feature_flags":               map[string]bool{},
		"skill_sources":               []string{},
		"allowed_providers":           []string{},
		"image_hosts":                 []string{},
		"deployment_hosts":            map[string]string{},
	}
}

func (d *Deps) handleGetPublicConfig(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, d.publicConfig())
}
