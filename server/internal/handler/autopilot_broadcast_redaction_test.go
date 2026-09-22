package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func TestAutopilotTriggerBroadcastsCarryNoWebhookCredential(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	apID := createAutopilotAs(t, "", "ap-broadcast-redaction")
	ws := "?workspace_id=" + testWorkspaceID

	var mu sync.Mutex
	var broadcasts []AutopilotTriggerResponse
	testHandler.Bus.Subscribe(protocol.EventAutopilotUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || payload["autopilot_id"] != apID {
			return
		}
		trigger, ok := payload["trigger"].(AutopilotTriggerResponse)
		if !ok {
			return
		}
		mu.Lock()
		broadcasts = append(broadcasts, trigger)
		mu.Unlock()
	})
	lastBroadcast := func(t *testing.T) AutopilotTriggerResponse {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if len(broadcasts) == 0 {
			t.Fatal("no autopilot:updated event was published; the UI would never refetch")
		}
		return broadcasts[len(broadcasts)-1]
	}

	call := func(t *testing.T, name string, handler http.HandlerFunc, req *http.Request, want int) AutopilotTriggerResponse {
		t.Helper()
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != want {
			t.Fatalf("%s: expected %d, got %d: %s", name, want, w.Code, w.Body.String())
		}
		var resp AutopilotTriggerResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}

		if resp.WebhookToken == nil || *resp.WebhookToken == "" {
			t.Fatalf("%s: HTTP response carries no webhook token; the broadcast assertion would prove nothing", name)
		}
		event := lastBroadcast(t)
		if event.WebhookToken != nil || event.WebhookPath != nil || event.WebhookURL != nil {
			t.Errorf("%s: broadcast leaked the webhook credential (token/path/url) — every workspace member receives this event", name)
		}
		if event.ID == "" {
			t.Errorf("%s: broadcast carries no trigger id; clients cannot tell what to refetch", name)
		}
		return resp
	}

	created := call(t, "create-webhook-trigger", testHandler.CreateAutopilotTrigger,
		withURLParam(newRequest("POST", "/api/autopilots/"+apID+"/triggers"+ws, map[string]any{"kind": "webhook", "label": "ci"}), "id", apID),
		http.StatusCreated)

	call(t, "update-trigger", testHandler.UpdateAutopilotTrigger,
		withURLParams(newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+created.ID+ws, map[string]any{"enabled": false}), "id", apID, "triggerId", created.ID),
		http.StatusOK)

	call(t, "rotate-webhook-token", testHandler.RotateAutopilotTriggerWebhookToken,
		withURLParams(newRequest("POST", "/api/autopilots/"+apID+"/triggers/"+created.ID+"/rotate-token"+ws, nil), "id", apID, "triggerId", created.ID),
		http.StatusOK)

	call(t, "set-signing-secret", testHandler.SetAutopilotTriggerSigningSecret,
		withURLParams(newRequest("PUT", "/api/autopilots/"+apID+"/triggers/"+created.ID+"/signing-secret"+ws, map[string]any{"signing_secret": "0123456789abcdef"}), "id", apID, "triggerId", created.ID),
		http.StatusOK)
}
