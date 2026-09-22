package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/adanman/goosar/server/internal/daemonws"
	"github.com/adanman/goosar/server/internal/middleware"
)

type rpcResponseCapture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *rpcResponseCapture) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *rpcResponseCapture) WriteHeader(code int) { w.status = code }

func (w *rpcResponseCapture) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(b)
}

func (h *Handler) DaemonRPCHandler(ctx context.Context, identity daemonws.ClientIdentity, method string, body json.RawMessage) (int, json.RawMessage, error) {
	switch method {
	case "tasks.claim":
		return h.rpcClaimTasks(ctx, identity, body)
	default:
		return http.StatusNotFound, nil, fmt.Errorf("unknown rpc method %q", method)
	}
}

func (h *Handler) rpcClaimTasks(ctx context.Context, identity daemonws.ClientIdentity, body json.RawMessage) (int, json.RawMessage, error) {
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}
	reqCtx := ctx

	if identity.DaemonID != "" {
		reqCtx = middleware.WithDaemonContext(reqCtx, identity.PrimaryWorkspaceID(), identity.DaemonID)
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, "/api/daemon/tasks/claim", bytes.NewReader(body))
	if err != nil {
		return http.StatusInternalServerError, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if identity.UserID != "" {
		req.Header.Set("X-User-ID", identity.UserID)
	}
	if identity.Capabilities != "" {
		req.Header.Set("X-Client-Capabilities", identity.Capabilities)
	}
	if identity.ClientVersion != "" {
		req.Header.Set("X-Client-Version", identity.ClientVersion)
	}

	rec := &rpcResponseCapture{}

	claim := middleware.ClientMetadata(RequireMinDaemonVersion(http.HandlerFunc(h.ClaimTasksByRuntime)))
	claim.ServeHTTP(rec, req)
	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	return status, json.RawMessage(rec.body.Bytes()), nil
}
