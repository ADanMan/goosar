package agent

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// ownerOnlyFields — contract §10.2: изменяет только владелец агента, даже
// если вызывающий — владелец/админ воркспейса.
var ownerOnlyFields = []string{"instructions", "custom_args", "runtime_config", "runtime_id",
	"permission_mode", "invocation_targets", "visibility", "composio_toolkit_allowlist"}

func (d *Deps) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	current, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	if !vc.canManage(current) {
		httpapi.Forbidden(w, "only the agent owner or workspace owner/admin can update it")
		return
	}
	body, err := httpapi.ReadBody(r)
	if err != nil {
		httpapi.BadRequest(w, "invalid body")
		return
	}
	fields, err := parseFields(body)
	if err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	isOwner := vc.isOwner(current)

	next := current
	var newTargets []InvocationTarget
	haveTargets := false
	var newSkip403 bool

	// ownerOnly проверяет "поле реально изменилось и вызывающий — не
	// владелец" -> 403 (см. package doc handlers_update.go выше);
	// изменение на то же значение молча игнорируется (contract §10.2).
	ownerOnlyChange := func(name string, changed bool) bool {
		if !changed || isOwner {
			return true
		}
		for _, f := range ownerOnlyFields {
			if f == name {
				newSkip403 = true
				return false
			}
		}
		return true
	}

	if _, uerr := decodeField(fields, "name", &next.Title); uerr != nil {
		httpapi.BadRequest(w, "invalid name")
		return
	}
	if present, uerr := decodeField(fields, "description", &next.Summary); uerr != nil {
		httpapi.BadRequest(w, "invalid description")
		return
	} else if present && len(next.Summary) > 255 {
		httpapi.BadRequest(w, "description must be at most 255 characters")
		return
	}

	var instructions string
	if present, uerr := decodeField(fields, "instructions", &instructions); uerr != nil {
		httpapi.BadRequest(w, "invalid instructions")
		return
	} else if present && ownerOnlyChange("instructions", instructions != current.Instructions) {
		next.Instructions = instructions
	}

	if present, uerr := decodeField(fields, "avatar_url", &next.AvatarURI); uerr != nil {
		httpapi.BadRequest(w, "invalid avatar_url")
		return
	} else {
		_ = present
	}

	var runtimeID string
	if present, uerr := decodeField(fields, "runtime_id", &runtimeID); uerr != nil {
		httpapi.BadRequest(w, "invalid runtime_id")
		return
	} else if present && ownerOnlyChange("runtime_id", runtimeID != current.ExecutorID) {
		rt, found, err := d.Store.getRuntime(r.Context(), wsID, runtimeID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if !found {
			httpapi.BadRequest(w, "runtime_id does not belong to this workspace")
			return
		}
		if !runtimeAccessible(rt, vc.ViewerID, vc.isOwnerOrAdmin()) {
			httpapi.Forbidden(w, "runtime is private and not accessible to the caller")
			return
		}
		next.ExecutorID = runtimeID
	}

	var runtimeConfig map[string]any
	if present, uerr := decodeField(fields, "runtime_config", &runtimeConfig); uerr != nil {
		httpapi.BadRequest(w, "invalid runtime_config")
		return
	} else if present {
		currentCfg := map[string]any{}
		openJSON(d.Store.cryptoKey, d.Store.cryptoKeyPrev, current.RuntimeConfigSeal, &currentCfg)
		changed := !jsonEqual(currentCfg, runtimeConfig)
		if ownerOnlyChange("runtime_config", changed) {
			sealed, _, err := sealJSON(d.Store.cryptoKey, orEmptyObject(runtimeConfig))
			if err != nil {
				httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
				return
			}
			next.RuntimeConfigSeal = sealed
		}
	}

	var customArgs []string
	if present, uerr := decodeField(fields, "custom_args", &customArgs); uerr != nil {
		httpapi.BadRequest(w, "invalid custom_args")
		return
	} else if present {
		raw, _ := json.Marshal(customArgs)
		changed := string(rawOr(current.CustomArgs, emptyArray())) != string(raw)
		if ownerOnlyChange("custom_args", changed) {
			next.CustomArgs = raw
		}
	}

	var mcpConfig map[string]any
	if present, uerr := decodeField(fields, "mcp_config", &mcpConfig); uerr != nil {
		httpapi.BadRequest(w, "invalid mcp_config")
		return
	} else if present {
		if mcpConfig == nil && !vc.canSeeSecrets(current) {
			// Значение показывалось этому вызывающему в скрытом виде
			// (mcp_config_redacted=true) — повторная отправка того же
			// "пустого" представления не должна стирать реальный секрет.
			// Решение этой сессии (см. server2/docs/decisions.md, T-028).
		} else if mcpConfig == nil {
			next.McpConfigSeal = nil
			next.McpConfigEncrypted = false
		} else {
			sealed, encrypted, err := sealJSON(d.Store.cryptoKey, mcpConfig)
			if err != nil {
				httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
				return
			}
			next.McpConfigSeal = sealed
			next.McpConfigEncrypted = encrypted
		}
	}

	var permissionMode string
	permissionPresent, uerr := decodeField(fields, "permission_mode", &permissionMode)
	if uerr != nil {
		httpapi.BadRequest(w, "invalid permission_mode")
		return
	}
	var visibility string
	visibilityPresent, uerr := decodeField(fields, "visibility", &visibility)
	if uerr != nil {
		httpapi.BadRequest(w, "invalid visibility")
		return
	}
	if _, present, uerr := decodeTargets(fields, &newTargets); uerr != nil {
		httpapi.BadRequest(w, "invalid invocation_targets")
		return
	} else {
		haveTargets = present
	}
	if permissionPresent || visibilityPresent {
		mode, targets := permissionFromRequest(pick(permissionPresent, permissionMode), pick(visibilityPresent, visibility), newTargets)
		changed := mode != current.PermissionMode
		if ownerOnlyChange("permission_mode", changed) {
			next.PermissionMode = mode
			if haveTargets || mode != current.PermissionMode {
				newTargets = targets
				haveTargets = true
			}
		}
	}

	var status string
	if present, uerr := decodeField(fields, "status", &status); uerr != nil {
		httpapi.BadRequest(w, "invalid status")
		return
	} else if present {
		next.Status = status
	}

	var maxConcurrent int
	if present, uerr := decodeField(fields, "max_concurrent_tasks", &maxConcurrent); uerr != nil {
		httpapi.BadRequest(w, "invalid max_concurrent_tasks")
		return
	} else if present {
		next.MaxConcurrentTasks = maxConcurrent
	}

	var model string
	if present, uerr := decodeField(fields, "model", &model); uerr != nil {
		httpapi.BadRequest(w, "invalid model")
		return
	} else if present {
		next.Model = model
	}
	var thinkingLevel string
	if present, uerr := decodeField(fields, "thinking_level", &thinkingLevel); uerr != nil {
		httpapi.BadRequest(w, "invalid thinking_level")
		return
	} else if present {
		next.ThinkingLevel = thinkingLevel
	}
	var serviceTier string
	if present, uerr := decodeField(fields, "service_tier", &serviceTier); uerr != nil {
		httpapi.BadRequest(w, "invalid service_tier")
		return
	} else if present {
		next.ServiceTier = serviceTier
	}

	var composio []string
	if present, uerr := decodeField(fields, "composio_toolkit_allowlist", &composio); uerr != nil {
		httpapi.BadRequest(w, "invalid composio_toolkit_allowlist")
		return
	} else if present {
		raw, _ := json.Marshal(composio)
		if composio == nil {
			raw = nil
		}
		changed := string(current.ComposioAllowlist) != string(raw)
		if ownerOnlyChange("composio_toolkit_allowlist", changed) {
			next.ComposioAllowlist = raw
		}
	}

	if newSkip403 {
		httpapi.Forbidden(w, "only the agent owner can change this field")
		return
	}

	if next.Title != current.Title {
		taken, err := d.Store.NameTaken(r.Context(), wsID, next.Title, current.ID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if taken {
			httpapi.WriteError(w, http.StatusConflict, "an agent with this name already exists", "agent_name_taken")
			return
		}
	}

	if err := d.Store.SaveCore(r.Context(), next); err != nil {
		if errors.Is(err, ErrNameTaken) {
			httpapi.WriteError(w, http.StatusConflict, "an agent with this name already exists", "agent_name_taken")
			return
		}
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if haveTargets {
		if err := d.Store.SetInvocationTargets(r.Context(), current.ID, newTargets); err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}
	if next.ExecutorID != current.ExecutorID {
		if err := d.Store.ClearDisabledRuntimeSkillsForOtherExecutor(r.Context(), current.ID, next.ExecutorID); err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}

	updated, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	view, err := d.fullView(r, vc, updated)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "agent:status", map[string]any{"agent": view})
	httpapi.WriteJSON(w, http.StatusOK, view)
}

func decodeTargets(f fieldSet, out *[]InvocationTarget) (value []InvocationTarget, present bool, err error) {
	present, err = decodeField(f, "invocation_targets", out)
	return *out, present, err
}

func pick(present bool, v string) string {
	if present {
		return v
	}
	return ""
}

func jsonEqual(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}
