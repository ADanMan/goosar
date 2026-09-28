// workspaces.go — три read-only маршрута контракта, объединённых общей
// заботой "сообщить демону что-то про воркспейс, к которому он привязан":
// список пространств вызывающего (GET /api/daemon/workspaces, с conditional
// GET по ETag), список репозиториев воркспейса + их дешёвый отпечаток
// (GET .../repos), включённые custom runtime-профили (GET .../runtime-profiles).
package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// --- GET /api/daemon/workspaces ---------------------------------------------

// daemonWorkspaceEntry — DaemonWorkspace контракта.
type daemonWorkspaceEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (d *Deps) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	entries, status, msg := d.resolveDaemonWorkspaces(r, actor)
	if status != http.StatusOK {
		httpapi.WriteError(w, status, msg, "")
		return
	}
	writeWithETag(w, r, entries)
}

// resolveDaemonWorkspaces — ветка "человек" vs "daemon-токен" разнесена в
// отдельные небольшие запросы, чтобы каждая читалась независимо от условия.
func (d *Deps) resolveDaemonWorkspaces(r *http.Request, actor *httpapi.Actor) (entries []daemonWorkspaceEntry, status int, msg string) {
	if actor.IsHuman {
		return d.humanWorkspaces(r, actor.UserID)
	}
	return d.boundDaemonWorkspace(r, actor.TaskWorkspaceID)
}

func (d *Deps) humanWorkspaces(r *http.Request, accountID string) ([]daemonWorkspaceEntry, int, string) {
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT s.id, s.ws_title FROM spaces s
		JOIN space_members sm ON sm.workspace_id = s.id
		WHERE sm.account_id = $1 ORDER BY s.ws_title`, accountID)
	if err != nil {
		return nil, http.StatusInternalServerError, "internal error"
	}
	defer rows.Close()
	entries := []daemonWorkspaceEntry{}
	for rows.Next() {
		var e daemonWorkspaceEntry
		if scanErr := rows.Scan(&e.ID, &e.Name); scanErr != nil {
			return nil, http.StatusInternalServerError, "internal error"
		}
		entries = append(entries, e)
	}
	return entries, http.StatusOK, ""
}

func (d *Deps) boundDaemonWorkspace(r *http.Request, workspaceID string) ([]daemonWorkspaceEntry, int, string) {
	if workspaceID == "" {
		return nil, http.StatusUnauthorized, "daemon workspace identity required"
	}
	var e daemonWorkspaceEntry
	err := d.DB.Pool.QueryRow(r.Context(), `SELECT id, ws_title FROM spaces WHERE id = $1`, workspaceID).Scan(&e.ID, &e.Name)
	switch {
	case err == nil:
		return []daemonWorkspaceEntry{e}, http.StatusOK, ""
	case store.IsNoRows(err):
		return nil, http.StatusNotFound, "bound workspace not found"
	default:
		return nil, http.StatusInternalServerError, "internal error"
	}
}

// writeWithETag — conditional GET (weak etag over the JSON body, contract:
// "Поддерживает ETag/If-None-Match").
func writeWithETag(w http.ResponseWriter, r *http.Request, v any) {
	body, _ := json.Marshal(v)
	etag := `"` + digestOf(string(body))[:16] + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	httpapi.WriteJSON(w, http.StatusOK, v)
}

// --- GET /api/daemon/workspaces/{workspaceId}/repos -------------------------

type workspaceRepo struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// workspaceRepos — текущий список репозиториев воркспейса плюс дешёвая
// "версия" (sha256 отсортированных URL), чтобы демон мог сравнить состояние
// без пересылки всего списка.
func (d *Deps) workspaceRepos(ctx context.Context, workspaceID string) ([]workspaceRepo, string, json.RawMessage, error) {
	var repoRefs, wsSettings []byte
	if err := d.DB.Pool.QueryRow(ctx, `SELECT ws_repo_refs, ws_settings FROM spaces WHERE id = $1`, workspaceID).
		Scan(&repoRefs, &wsSettings); err != nil {
		return nil, "", nil, err
	}
	var repos []workspaceRepo
	_ = json.Unmarshal(repoRefs, &repos)
	version := reposFingerprint(repos)
	if len(wsSettings) == 0 {
		wsSettings = []byte(`{}`)
	}
	return repos, version, wsSettings, nil
}

// reposFingerprint — отпечаток набора URL, устойчивый к их порядку в jsonb.
func reposFingerprint(repos []workspaceRepo) string {
	urls := make([]string, len(repos))
	for i, repo := range repos {
		urls[i] = repo.URL
	}
	sort.Strings(urls)
	return digestOf(strings.Join(urls, "\x00"))
}

func (d *Deps) handleGetWorkspaceRepos(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := d.requireWorkspaceAccess(w, r, workspaceID); !ok {
		return
	}
	repos, version, settings, err := d.workspaceRepos(r.Context(), workspaceID)
	switch {
	case store.IsNoRows(err):
		httpapi.NotFound(w, "workspace not found")
	case err != nil:
		d.internalErr(w, err)
	default:
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{
			"workspace_id": workspaceID, "repos": repos, "repos_version": version, "settings": settings,
		})
	}
}

// --- GET /api/daemon/workspaces/{workspaceId}/runtime-profiles --------------

// handleListRuntimeProfiles — только включённые: daemon не интересуют
// выключенные профили.
func (d *Deps) handleListRuntimeProfiles(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := d.requireWorkspaceAccess(w, r, workspaceID); !ok {
		return
	}
	all, err := d.Workspace.ListRuntimeProfiles(r.Context(), workspaceID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	enabled := make([]any, 0, len(all))
	for _, profile := range all {
		if profile.Enabled {
			enabled = append(enabled, profile)
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"workspace_id": workspaceID, "runtime_profiles": enabled})
}
