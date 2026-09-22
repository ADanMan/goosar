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

type ModelListStatus string

const (
	ModelListPending   ModelListStatus = "pending"
	ModelListRunning   ModelListStatus = "running"
	ModelListCompleted ModelListStatus = "completed"
	ModelListFailed    ModelListStatus = "failed"
	ModelListTimeout   ModelListStatus = "timeout"
)

type ModelListRequest struct {
	ID           string          `json:"id"`
	RuntimeID    string          `json:"runtime_id"`
	Status       ModelListStatus `json:"status"`
	Models       []ModelEntry    `json:"models,omitempty"`
	Supported    bool            `json:"supported"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	RunStartedAt *time.Time      `json:"-"`
}

type ModelEntry struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Default      bool               `json:"default,omitempty"`
	Thinking     *ModelThinking     `json:"thinking,omitempty"`
	ServiceTiers []ModelServiceTier `json:"service_tiers,omitempty"`
}

type ModelServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ModelThinking struct {
	SupportedLevels []ThinkingLevel `json:"supported_levels"`
	DefaultLevel    string          `json:"default_level,omitempty"`
}

type ThinkingLevel struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

const (
	modelListPendingTimeout = 30 * time.Second

	modelListRunningTimeout = 60 * time.Second

	modelListStoreRetention = 2 * time.Minute
)

type ModelListStore interface {
	Create(ctx context.Context, runtimeID string) (*ModelListRequest, error)
	Get(ctx context.Context, id string) (*ModelListRequest, error)

	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*ModelListRequest, error)
	Complete(ctx context.Context, id string, models []ModelEntry, supported bool) error
	Fail(ctx context.Context, id string, errMsg string) error
}

func applyModelListTimeout(req *ModelListRequest, now time.Time) bool {
	switch req.Status {
	case ModelListPending:
		if now.Sub(req.CreatedAt) > modelListPendingTimeout {
			req.Status = ModelListTimeout
			req.Error = "daemon did not respond within 30 seconds"
			req.UpdatedAt = now
			return true
		}
	case ModelListRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > modelListRunningTimeout {
			req.Status = ModelListTimeout
			req.Error = "daemon did not finish within 60 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

type InMemoryModelListStore struct {
	mu       sync.Mutex
	requests map[string]*ModelListRequest
}

func NewInMemoryModelListStore() *InMemoryModelListStore {
	return &InMemoryModelListStore{requests: make(map[string]*ModelListRequest)}
}

func (s *InMemoryModelListStore) Create(_ context.Context, runtimeID string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > modelListStoreRetention {
			delete(s.requests, id)
		}
	}

	now := time.Now()
	req := &ModelListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    ModelListPending,

		Supported: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryModelListStore) Get(_ context.Context, id string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyModelListTimeout(req, time.Now())
	return req, nil
}

func (s *InMemoryModelListStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyModelListTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == ModelListPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryModelListStore) PopPending(_ context.Context, runtimeID string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *ModelListRequest
	now := time.Now()
	for _, req := range s.requests {
		applyModelListTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == ModelListPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = ModelListRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryModelListStore) Complete(_ context.Context, id string, models []ModelEntry, supported bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ModelListCompleted
		req.Models = models
		req.Supported = supported
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryModelListStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ModelListFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

func modelListRequestTerminal(status ModelListStatus) bool {
	return status == ModelListCompleted || status == ModelListFailed || status == ModelListTimeout
}

func (h *Handler) InitiateListModels(w http.ResponseWriter, r *http.Request) {
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
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}
	if rt.Status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}

	req, err := h.ModelListStore.Create(r.Context(), uuidToString(rt.ID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue model list request: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) GetModelListRequest(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "requestId")

	req, err := h.ModelListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) ReportModelListResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")

	existing, err := h.ModelListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if modelListRequestTerminal(existing.Status) {
		slog.Debug("ignoring stale model list report", "runtime_id", runtimeID, "request_id", requestID, "status", existing.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var body struct {
		Status    string       `json:"status"`
		Models    []ModelEntry `json:"models"`
		Supported *bool        `json:"supported"`
		Error     string       `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Status == "completed" {

		supported := true
		if body.Supported != nil {
			supported = *body.Supported
		}
		if err := h.ModelListStore.Complete(r.Context(), requestID, body.Models, supported); err != nil {

			slog.Error("ModelListStore Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	} else {
		if err := h.ModelListStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("ModelListStore Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	}

	slog.Debug("model list report", "runtime_id", runtimeID, "request_id", requestID, "status", body.Status, "count", len(body.Models))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
