package autopilot

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// --- runs (§7.3: "Run — факт срабатывания автопилота") -------------------

func (d *Deps) handleTriggerAutopilot(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !d.requireWriteAccess(w, r, id, actor.UserID, member.Role) {
		return
	}
	raw, err := d.Store.GetRaw(r.Context(), id)
	if err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if raw.Status != "active" {
		httpapi.WriteError(w, http.StatusBadRequest, "autopilot is not active", "autopilot_not_active")
		return
	}
	result, err := d.Dispatcher.DispatchManual(r.Context(), id)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:run_start", map[string]any{"run": result.Run.Full()})
	httpapi.WriteJSON(w, http.StatusOK, result.Run.Full())
}

func (d *Deps) handleListRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.Resolver.RequireMember(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := d.Store.GetRaw(r.Context(), id); err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	page := queryPage(r.URL.Query())
	runs, total, err := d.Store.ListRuns(r.Context(), id, page.limit, page.offset)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	resp := struct {
		Runs  []Run `json:"runs"`
		Total int   `json:"total"`
	}{Runs: runs, Total: total}
	if resp.Runs == nil {
		resp.Runs = []Run{}
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

func (d *Deps) handleGetRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.Resolver.RequireMember(w, r); !ok {
		return
	}
	id, runID := r.PathValue("id"), r.PathValue("runId")
	run, err := d.Store.GetRun(r.Context(), id, runID)
	if err == ErrRunNotFound {
		httpapi.NotFound(w, "run not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, run.Full())
}

// --- deliveries (§7.3: "Delivery — факт получения входящего вебхука") ----

func (d *Deps) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.Resolver.RequireMember(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := d.Store.GetRaw(r.Context(), id); err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	page := queryPage(r.URL.Query())
	deliveries, total, err := d.Store.ListDeliveries(r.Context(), id, page.limit, page.offset)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	resp := struct {
		Deliveries []Delivery `json:"deliveries"`
		Total      int        `json:"total"`
	}{Deliveries: deliveries, Total: total}
	if resp.Deliveries == nil {
		resp.Deliveries = []Delivery{}
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

func (d *Deps) handleGetDelivery(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.Resolver.RequireMember(w, r); !ok {
		return
	}
	id, deliveryID := r.PathValue("id"), r.PathValue("deliveryId")
	del, err := d.Store.GetDelivery(r.Context(), id, deliveryID)
	if err == ErrDeliveryNotFound {
		httpapi.NotFound(w, "delivery not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, del.Full())
}

func (d *Deps) handleReplayDelivery(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	autopilotID, deliveryID := r.PathValue("id"), r.PathValue("deliveryId")
	if !d.requireWriteAccess(w, r, autopilotID, actor.UserID, member.Role) {
		return
	}

	raw, err := d.Store.GetRaw(r.Context(), autopilotID)
	if err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	original, err := d.Store.GetDelivery(r.Context(), autopilotID, deliveryID)
	if err == ErrDeliveryNotFound {
		httpapi.NotFound(w, "delivery not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	trigger, err := d.Store.GetTrigger(r.Context(), autopilotID, original.TriggerID)
	if err == ErrTriggerNotFound {
		httpapi.WriteError(w, http.StatusBadRequest, "trigger no longer exists", "trigger_missing")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	// Условия реплея доставки (contract §7.3: нельзя реплеить доставку с
	// провалившей проверку подписи, без сохранённого тела, либо чей триггер
	// сейчас выключен, либо автопилот не active) — данные-таблицей, а не
	// цепочкой одинаковых if-ов, чтобы причина отказа читалась одним взглядом
	// на replayBlockers ниже, вместо пяти похожих guard clause подряд.
	blockers := []struct {
		blocked bool
		message string
		code    string
	}{
		{raw.Status != "active", "autopilot is not active", "autopilot_not_active"},
		{original.SignatureStatus == "invalid" || original.Status == "rejected",
			"delivery failed signature verification and cannot be replayed", "delivery_signature_invalid"},
		{original.RawBody == nil, "delivery has no stored body to replay", "delivery_body_missing"},
		{!trigger.Enabled, "trigger is currently disabled", "trigger_disabled"},
	}
	for _, b := range blockers {
		if b.blocked {
			httpapi.WriteError(w, http.StatusBadRequest, b.message, b.code)
			return
		}
	}

	envelope := normalizeWebhookEnvelope(original.Event, []byte(*original.RawBody))
	matched := matchEventFilters(trigger, envelope)

	status := "ignored"
	var runID *string
	if matched {
		status = "dispatched"
		result, err := d.Dispatcher.DispatchWebhook(r.Context(), autopilotID, trigger.ID, envelope)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		runID = &result.Run.ID
		d.publish(member.WorkspaceID, "autopilot:run_start", map[string]any{"run": result.Run})
	}

	replay, err := d.Store.CreateDelivery(r.Context(), CreateDeliveryParams{
		WorkspaceID: member.WorkspaceID, AutopilotID: autopilotID, TriggerID: trigger.ID,
		Provider: original.Provider, Event: original.Event,
		// реплей инициирует владелец автопилота через API, не внешний
		// провайдер — исходная подпись уже проверена при первом приёме
		// доставки (contract: нельзя реплеить доставку, провалившую
		// проверку подписи, см. отказы выше), повторно проверять нечего.
		SignatureStatus:        "not_required",
		Status:                 status,
		ReplayedFromDeliveryID: &deliveryID,
		RawBody:                original.RawBody,
		AutopilotRunID:         runID,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, replay.Full())
}
