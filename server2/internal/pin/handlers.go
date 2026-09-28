package pin

import (
	"errors"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

func (d *Deps) handleListPins(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	list, err := d.Store.List(r.Context(), member.WorkspaceID, member.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Pin{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

type createPinRequest struct {
	ItemType string `json:"item_type"`
	ItemID   string `json:"item_id"`
}

func (d *Deps) handleCreatePin(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	var req createPinRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.ItemType != "issue" && req.ItemType != "project" {
		httpapi.BadRequest(w, "item_type must be issue or project")
		return
	}
	if req.ItemID == "" {
		httpapi.BadRequest(w, "item_id is required")
		return
	}
	var exists bool
	var err error
	if req.ItemType == "issue" {
		exists, err = d.Store.IssueExists(r.Context(), member.WorkspaceID, req.ItemID)
	} else {
		exists, err = d.Store.ProjectExists(r.Context(), member.WorkspaceID, req.ItemID)
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !exists {
		httpapi.NotFound(w, "issue/project not found")
		return
	}
	p, err := d.Store.Create(r.Context(), member.WorkspaceID, member.UserID, req.ItemType, req.ItemID)
	if errors.Is(err, ErrAlreadyExist) {
		httpapi.WriteError(w, http.StatusConflict, "item is already pinned", "pin_already_exists")
		return
	}
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "issue/project not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "pin:created", Payload: map[string]any{"pin": p}})
	}
	httpapi.WriteJSON(w, http.StatusCreated, p)
}

type reorderItem struct {
	ID       string  `json:"id"`
	Position float64 `json:"position"`
}

type reorderPinsRequest struct {
	Items []reorderItem `json:"items"`
}

func (d *Deps) handleReorderPins(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	var req reorderPinsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || len(req.Items) == 0 {
		httpapi.BadRequest(w, "items is required")
		return
	}
	for _, item := range req.Items {
		// По одной записи, без общей атомарности между ними (контракт) —
		// ошибка на одном элементе не откатывает уже применённые.
		_ = d.Store.UpdatePosition(r.Context(), member.UserID, item.ID, item.Position)
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "pin:reordered", Payload: map[string]any{"items": req.Items}})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleDeletePin(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	itemType, itemID := r.PathValue("itemType"), r.PathValue("itemId")
	if err := d.Store.Delete(r.Context(), member.WorkspaceID, member.UserID, itemType, itemID); err != nil && !errors.Is(err, ErrNotFound) {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "pin:deleted", Payload: map[string]any{"item_type": itemType, "item_id": itemID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// Register регистрирует операции тега Pins.
func Register(router *httpapi.Router, deps *Deps) {
	table := [...]struct {
		method  string
		pattern string
		handler http.HandlerFunc
	}{
		{http.MethodGet, "/api/pins", deps.handleListPins},
		{http.MethodPost, "/api/pins", deps.handleCreatePin},
		{http.MethodPut, "/api/pins/reorder", deps.handleReorderPins},
		{http.MethodDelete, "/api/pins/{itemType}/{itemId}", deps.handleDeletePin},
	}
	for _, rt := range table {
		router.Handle(rt.method, rt.pattern, rt.handler)
	}
}
