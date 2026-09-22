package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/daemon/repocache"
)

type HealthResponse struct {
	Status string `json:"status"`
	PID    int    `json:"pid"`

	OS              string `json:"os"`
	Uptime          string `json:"uptime"`
	DaemonID        string `json:"daemon_id"`
	DeviceName      string `json:"device_name"`
	ServerURL       string `json:"server_url"`
	CLIVersion      string `json:"cli_version"`
	ActiveTaskCount int64  `json:"active_task_count"`

	Draining   bool              `json:"draining"`
	Agents     []string          `json:"agents"`
	Workspaces []healthWorkspace `json:"workspaces"`
}

type healthWorkspace struct {
	ID       string   `json:"id"`
	Runtimes []string `json:"runtimes"`
}

func (d *Daemon) listenHealth() (net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", d.cfg.HealthPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("another daemon is already running on %s: %w", addr, err)
	}
	return ln, nil
}

type repoCheckoutRequest struct {
	URL          string `json:"url"`
	WorkspaceID  string `json:"workspace_id"`
	WorkDir      string `json:"workdir"`
	Ref          string `json:"ref,omitempty"`
	AgentName    string `json:"agent_name"`
	TaskID       string `json:"task_id"`
	CheckoutMode string `json:"checkout_mode,omitempty"`
}

type activeRepoCheckoutTask struct {
	WorkspaceID string
	TaskID      string
	AgentID     string
	AgentName   string
	WorkDir     string
}

func (d *Daemon) registerActiveRepoCheckoutTask(token string, task activeRepoCheckoutTask) {
	d.repoCheckoutTasksMu.Lock()
	defer d.repoCheckoutTasksMu.Unlock()
	if d.repoCheckoutTasks == nil {
		d.repoCheckoutTasks = make(map[string]activeRepoCheckoutTask)
	}
	d.repoCheckoutTasks[token] = task
}

func (d *Daemon) clearActiveRepoCheckoutTask(token string) {
	d.repoCheckoutTasksMu.Lock()
	delete(d.repoCheckoutTasks, token)
	d.repoCheckoutTasksMu.Unlock()
}

func (d *Daemon) activeRepoCheckoutTask(r *http.Request) (activeRepoCheckoutTask, bool) {
	const bearer = "Bearer "
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, bearer) {
		return activeRepoCheckoutTask{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, bearer))
	if token == "" {
		return activeRepoCheckoutTask{}, false
	}
	d.repoCheckoutTasksMu.RLock()
	task, ok := d.repoCheckoutTasks[token]
	d.repoCheckoutTasksMu.RUnlock()
	return task, ok
}

func authorizeRepoCheckoutWorkDir(activeRoot, requested string) (string, error) {
	root, err := filepath.Abs(activeRoot)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	workdir, err := filepath.Abs(requested)
	if err != nil {
		return "", err
	}
	workdir, err = filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, workdir)
	if err != nil || !filepath.IsLocal(rel) {
		return "", errors.New("workdir is outside the active task workdir")
	}
	return workdir, nil
}

func (d *Daemon) healthHandler(startedAt time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		var wsList []healthWorkspace
		for id, ws := range d.workspaces {
			wsList = append(wsList, healthWorkspace{
				ID:       id,
				Runtimes: ws.runtimeIDs,
			})
		}
		d.mu.Unlock()

		agents := make([]string, 0, len(d.cfg.Agents))
		for name := range d.cfg.Agents {
			agents = append(agents, name)
		}

		status := "starting"
		if d.ready.Load() {
			status = "running"
		}

		resp := HealthResponse{
			Status:          status,
			PID:             os.Getpid(),
			OS:              runtime.GOOS,
			Uptime:          time.Since(startedAt).Truncate(time.Second).String(),
			DaemonID:        d.cfg.DaemonID,
			DeviceName:      d.cfg.DeviceName,
			ServerURL:       d.cfg.ServerBaseURL,
			CLIVersion:      d.cfg.CLIVersion,
			ActiveTaskCount: d.activeTasks.Load(),
			Draining:        d.draining.Load(),
			Agents:          agents,
			Workspaces:      wsList,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func (d *Daemon) shutdownHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		force := r.URL.Query().Get("force") == "1"
		d.forceShutdown.Store(force)
		w.Header().Set("Content-Type", "application/json")
		status := "draining"
		if force {
			status = "shutting down"
		}
		json.NewEncoder(w).Encode(map[string]string{"status": status})
		if d.cancelFunc != nil {

			go d.cancelFunc()
		}
	}
}

func (d *Daemon) serveHealth(ctx context.Context, ln net.Listener, startedAt time.Time) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.healthHandler(startedAt))
	mux.HandleFunc("/shutdown", d.shutdownHandler())
	mux.HandleFunc("/repo/checkout", d.repoCheckoutHandler())

	srv := &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	d.logger.Info("health server listening", "addr", ln.Addr().String())
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		d.logger.Warn("health server error", "error", err)
	}
}

func (d *Daemon) repoCheckoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		activeTask, ok := d.activeRepoCheckoutTask(r)
		if !ok {
			http.Error(w, "repo checkout requires an active task credential", http.StatusUnauthorized)
			return
		}

		var req repoCheckoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		req.URL = strings.TrimSpace(req.URL)
		if req.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID == "" {
			http.Error(w, "workspace_id is required", http.StatusBadRequest)
			return
		}
		if req.WorkDir == "" {
			http.Error(w, "workdir is required", http.StatusBadRequest)
			return
		}
		if req.CheckoutMode != "" && req.CheckoutMode != repoCheckoutModeIsolated {
			http.Error(w, "invalid checkout_mode", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID != activeTask.WorkspaceID || req.TaskID != activeTask.TaskID {
			http.Error(w, "repo checkout task context does not match the active task", http.StatusForbidden)
			return
		}
		authorizedWorkDir, authErr := authorizeRepoCheckoutWorkDir(activeTask.WorkDir, req.WorkDir)
		if authErr != nil {
			http.Error(w, "repo checkout workdir is not owned by the active task", http.StatusForbidden)
			return
		}

		req.WorkspaceID = activeTask.WorkspaceID
		req.TaskID = activeTask.TaskID
		req.AgentName = activeTask.AgentName
		req.WorkDir = authorizedWorkDir

		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}

		if err := d.ensureRepoReady(r.Context(), req.WorkspaceID, req.URL); err != nil {
			statusCode := http.StatusInternalServerError
			if errors.Is(err, ErrRepoNotConfigured) {
				statusCode = http.StatusBadRequest
			}
			d.logger.Error("repo checkout readiness failed", "workspace_id", req.WorkspaceID, "url", req.URL, "error", err)
			http.Error(w, err.Error(), statusCode)
			return
		}

		checkoutRef := strings.TrimSpace(req.Ref)
		if checkoutRef == "" {
			checkoutRef = d.taskRepoDefaultRef(req.WorkspaceID, req.TaskID, req.URL)
		}

		result, err := d.repoCache.CreateWorktree(repocache.WorktreeParams{
			WorkspaceID:         req.WorkspaceID,
			RepoURL:             req.URL,
			WorkDir:             req.WorkDir,
			Ref:                 checkoutRef,
			AgentName:           req.AgentName,
			TaskID:              req.TaskID,
			CoAuthoredByEnabled: d.workspaceCoAuthoredByEnabled(req.WorkspaceID),
			IsolatedGitMetadata: req.CheckoutMode == repoCheckoutModeIsolated,
		})
		if err != nil {
			d.logger.Error("repo checkout failed", "url", req.URL, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
