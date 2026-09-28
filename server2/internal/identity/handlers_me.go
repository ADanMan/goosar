// Package identity реализует тег Me контракта: собственный профиль
// пользователя, онбординг, CLI-токен, personal access tokens (/api/tokens) и
// статичный список шаблонов пространств. Persistence аккаунта переиспользует
// authn.Store (см. server2/internal/authn/userview.go) — таблица accounts
// принадлежит authn (сессии/коды тоже читают/пишут её), identity добавляет
// только HTTP-слой профиля.
package identity

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Deps держит всё, что нужно обработчикам тега Me. Authn не embed'ится
// напрямую (хотя мог бы), чтобы вызовы вида d.Authn.Store.* оставались явно
// видны в обработчиках — откуда на самом деле приходят данные профиля.
type Deps struct {
	Authn  *authn.Deps
	Logger *slog.Logger
}

// New строит Deps домена identity вокруг уже готового authn.Deps.
func New(authnDeps *authn.Deps, logger *slog.Logger) *Deps {
	return &Deps{Authn: authnDeps, Logger: logger}
}

func (d *Deps) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	user, err := d.Authn.Store.GetUserView(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, user)
}

var allowedLanguages = map[string]bool{"en": true, "zh-Hans": true, "ko": true, "ja": true, "ru": true}

type updateProfileRequest struct {
	Name               *string `json:"name"`
	AvatarURL          *string `json:"avatar_url"`
	Language           *string `json:"language"`
	ProfileDescription *string `json:"profile_description"`
	Timezone           *string `json:"timezone"`
}

func (d *Deps) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req updateProfileRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.Language != nil && !allowedLanguages[*req.Language] {
		httpapi.BadRequest(w, "unsupported language")
		return
	}
	if req.ProfileDescription != nil && len(*req.ProfileDescription) > 2000 {
		httpapi.BadRequest(w, "profile_description too long")
		return
	}
	user, err := d.Authn.Store.UpdateUserView(r.Context(), actor.UserID, authn.ProfilePatch{
		Name:               req.Name,
		AvatarURL:          req.AvatarURL,
		Language:           req.Language,
		ProfileDescription: req.ProfileDescription,
		Timezone:           req.Timezone,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, user)
}

type onboardingPatchRequest struct {
	Questionnaire json.RawMessage `json:"questionnaire"`
}

func (d *Deps) handleUpdateOnboardingQuestionnaire(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req onboardingPatchRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if len(req.Questionnaire) == 0 {
		req.Questionnaire = []byte("{}")
	}
	user, err := d.Authn.Store.MergeOnboardingQuestionnaire(r.Context(), actor.UserID, req.Questionnaire)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, user)
}

func (d *Deps) handleCompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	// completion_path/workspace_id — только аналитика в контракте, не влияют
	// на форму ответа; тело не обязано присутствовать.
	var req struct {
		CompletionPath string `json:"completion_path"`
		WorkspaceID    string `json:"workspace_id"`
	}
	_ = httpapi.DecodeJSON(r, &req)

	user, err := d.Authn.Store.CompleteOnboarding(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, user)
}

type cloudWaitlistRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

func (d *Deps) handleJoinCloudWaitlist(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req cloudWaitlistRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.Email == "" || len(req.Email) > 254 {
		httpapi.BadRequest(w, "email is required")
		return
	}
	// Лист ожидания облака — вне объёма T-026 (нет облачного рантайма);
	// запрос принимается и подтверждается профилем без побочных эффектов.
	user, err := d.Authn.Store.GetUserView(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.Logger.Info("me: заявка в лист ожидания облака", "user_id", actor.UserID, "email", req.Email)
	httpapi.WriteJSON(w, http.StatusOK, user)
}

func (d *Deps) notImplemented(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteNotImplemented(w, r)
}
