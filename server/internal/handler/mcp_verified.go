package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type McpVerifiedResult struct {
	Server string `json:"server"`

	Status string `json:"status"`
}

var (
	mcpVerifiedMu    sync.RWMutex
	mcpVerifiedStore = map[string]mcpVerifiedEntry{}
)

type mcpVerifiedEntry struct {
	result     McpVerifiedResult
	verifiedAt time.Time
}

func recordMcpVerified(runtimeID string, result McpVerifiedResult) {
	mcpVerifiedMu.Lock()
	defer mcpVerifiedMu.Unlock()
	mcpVerifiedStore[runtimeID] = mcpVerifiedEntry{result: result, verifiedAt: time.Now()}
}

func lookupMcpVerified(runtimeID string) (McpVerifiedResult, bool) {
	mcpVerifiedMu.RLock()
	defer mcpVerifiedMu.RUnlock()
	entry, ok := mcpVerifiedStore[runtimeID]
	return entry.result, ok
}

func resetMcpVerifiedStoreForTests() {
	mcpVerifiedMu.Lock()
	defer mcpVerifiedMu.Unlock()
	mcpVerifiedStore = map[string]mcpVerifiedEntry{}
}

func (h *Handler) ReportMcpVerified(w http.ResponseWriter, r *http.Request) {
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

	var req McpVerifiedResult
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Server) == "" {
		writeError(w, http.StatusBadRequest, "server is required")
		return
	}
	if strings.TrimSpace(req.Status) == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}
	if req.Status != "ok" && req.Status != "proxy_unreachable" &&
		!strings.HasPrefix(req.Status, "mcp_failed(") {
		writeError(w, http.StatusBadRequest, "status must be ok, proxy_unreachable, or mcp_failed(<reason>)")
		return
	}

	recordMcpVerified(uuidToString(runtimeUUID), req)
	w.WriteHeader(http.StatusNoContent)
}
