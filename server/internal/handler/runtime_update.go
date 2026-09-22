package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type UpdateStatus string

const (
	UpdatePending   UpdateStatus = "pending"
	UpdateRunning   UpdateStatus = "running"
	UpdateCompleted UpdateStatus = "completed"
	UpdateFailed    UpdateStatus = "failed"
	UpdateTimeout   UpdateStatus = "timeout"
)

type UpdateRequest struct {
	ID              string       `json:"id"`
	RuntimeID       string       `json:"runtime_id"`
	InitiatorUserID string       `json:"-"`
	Status          UpdateStatus `json:"status"`
	TargetVersion   string       `json:"target_version"`
	Output          string       `json:"output,omitempty"`
	Error           string       `json:"error,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	RunStartedAt    *time.Time   `json:"-"`
}

const (
	updatePendingTimeout = 120 * time.Second
	updateRunningTimeout = 150 * time.Second
	updateStoreRetention = 5 * time.Minute
)

type UpdateStore interface {
	Create(ctx context.Context, runtimeID, targetVersion, initiatorUserID string) (*UpdateRequest, error)
	Get(ctx context.Context, id string) (*UpdateRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*UpdateRequest, error)
	Complete(ctx context.Context, id string, output string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

func updateRequestTerminal(status UpdateStatus) bool {
	return status == UpdateCompleted || status == UpdateFailed || status == UpdateTimeout
}

func applyUpdateTimeout(req *UpdateRequest, now time.Time) bool {
	switch req.Status {
	case UpdatePending:
		if now.Sub(req.CreatedAt) > updatePendingTimeout {
			req.Status = UpdateTimeout
			req.Error = "daemon did not respond within 120 seconds"
			req.UpdatedAt = now
			return true
		}
	case UpdateRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > updateRunningTimeout {
			req.Status = UpdateTimeout
			req.Error = "update did not complete within 150 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

type InMemoryUpdateStore struct {
	mu       sync.Mutex
	requests map[string]*UpdateRequest
}

func NewInMemoryUpdateStore() *InMemoryUpdateStore {
	return &InMemoryUpdateStore{
		requests: make(map[string]*UpdateRequest),
	}
}

func (s *InMemoryUpdateStore) Create(_ context.Context, runtimeID, targetVersion, initiatorUserID string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > updateStoreRetention {
			delete(s.requests, id)
		}
	}

	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && (req.Status == UpdatePending || req.Status == UpdateRunning) {
			return nil, errUpdateInProgress
		}
	}

	req := &UpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		InitiatorUserID: initiatorUserID,
		Status:          UpdatePending,
		TargetVersion:   targetVersion,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	s.requests[req.ID] = req
	return req, nil
}

var errUpdateInProgress = &updateError{msg: "an update is already in progress for this runtime"}

type updateError struct{ msg string }

func (e *updateError) Error() string { return e.msg }

func (s *InMemoryUpdateStore) Get(_ context.Context, id string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyUpdateTimeout(req, time.Now())
	return req, nil
}

func (s *InMemoryUpdateStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyUpdateTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == UpdatePending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryUpdateStore) PopPending(_ context.Context, runtimeID string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *UpdateRequest
	now := time.Now()
	for _, req := range s.requests {
		applyUpdateTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == UpdatePending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = UpdateRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryUpdateStore) Complete(_ context.Context, id string, output string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = UpdateCompleted
		req.Output = output
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryUpdateStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = UpdateFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (h *Handler) InitiateUpdate(w http.ResponseWriter, r *http.Request) {

	if h.cfg.DeliveryProfile.IsPerimeter() {
		writeError(w, http.StatusForbidden, "self-update is disabled on this deployment (perimeter delivery profile); updates are delivered by your operator")
		return
	}

	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "only runtime owners and workspace admins can update runtimes")
		return
	}

	var req struct {
		TargetVersion string `json:"target_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetVersion == "" {
		writeError(w, http.StatusBadRequest, "target_version is required")
		return
	}

	update, err := h.UpdateStore.Create(
		r.Context(),
		uuidToString(rt.ID),
		req.TargetVersion,
		uuidToString(member.UserID),
	)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, update)
}

func (h *Handler) GetUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}
	updateID := chi.URLParam(r, "updateId")

	update, err := h.UpdateStore.Get(r.Context(), updateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load update: "+err.Error())
		return
	}
	if update == nil || update.RuntimeID != uuidToString(rt.ID) {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}

	if !canEditRuntime(member, rt) && update.InitiatorUserID != uuidToString(member.UserID) {
		writeError(w, http.StatusForbidden, "only runtime owners, workspace admins, and the update initiator can view this update")
		return
	}

	writeJSON(w, http.StatusOK, update)
}

func (h *Handler) ReportUpdateResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	updateID := chi.URLParam(r, "updateId")

	existing, err := h.UpdateStore.Get(r.Context(), updateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load update: "+err.Error())
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}
	if updateRequestTerminal(existing.Status) {
		slog.Debug("ignoring stale update report", "runtime_id", runtimeID, "update_id", updateID, "status", existing.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var req struct {
		Status string `json:"status"`
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.Status {
	case "completed":
		if err := h.UpdateStore.Complete(r.Context(), updateID, req.Output); err != nil {
			slog.Error("UpdateStore Complete failed", "error", err, "update_id", updateID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	case "failed":
		if err := h.UpdateStore.Fail(r.Context(), updateID, req.Error); err != nil {
			slog.Error("UpdateStore Fail failed", "error", err, "update_id", updateID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	case "running":

	default:
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
