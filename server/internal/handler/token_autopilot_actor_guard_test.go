package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestNewlyGatedRoutes_RejectTaskTokenActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	cases := []struct {
		name    string
		method  string
		pattern string
		path    string
		handler http.HandlerFunc
	}{
		{"tokens.list", "GET", "/api/tokens", "/api/tokens", testHandler.ListPersonalAccessTokens},
		{"tokens.create", "POST", "/api/tokens", "/api/tokens", testHandler.CreatePersonalAccessToken},
		{"tokens.renew", "POST", "/api/tokens/current/renew", "/api/tokens/current/renew", testHandler.RenewCurrentPersonalAccessToken},
		{"tokens.revoke", "DELETE", "/api/tokens/{id}", "/api/tokens/abc", testHandler.RevokePersonalAccessToken},
		{"autopilot.trigger.create", "POST", "/api/autopilots/{id}/triggers", "/api/autopilots/a/triggers", testHandler.CreateAutopilotTrigger},
		{"autopilot.trigger.update", "PATCH", "/api/autopilots/{id}/triggers/{triggerId}", "/api/autopilots/a/triggers/t", testHandler.UpdateAutopilotTrigger},
		{"autopilot.trigger.delete", "DELETE", "/api/autopilots/{id}/triggers/{triggerId}", "/api/autopilots/a/triggers/t", testHandler.DeleteAutopilotTrigger},
		{"autopilot.trigger.rotate", "POST", "/api/autopilots/{id}/triggers/{triggerId}/rotate-webhook-token", "/api/autopilots/a/triggers/t/rotate-webhook-token", testHandler.RotateAutopilotTriggerWebhookToken},
		{"autopilot.trigger.signing", "PUT", "/api/autopilots/{id}/triggers/{triggerId}/signing-secret", "/api/autopilots/a/triggers/t/signing-secret", testHandler.SetAutopilotTriggerSigningSecret},
		{"autopilot.collaborator.add", "POST", "/api/autopilots/{id}/collaborators", "/api/autopilots/a/collaborators", testHandler.AddAutopilotCollaborator},
		{"autopilot.collaborator.remove", "DELETE", "/api/autopilots/{id}/collaborators/{userId}", "/api/autopilots/a/collaborators/u", testHandler.RemoveAutopilotCollaborator},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			r.With(RequireHumanActor).MethodFunc(tc.method, tc.pattern, tc.handler)

			req := newRequest(tc.method, tc.path, nil)

			req.Header.Set("X-Actor-Source", "task_token")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("%s %s with X-Actor-Source=task_token: status = %d, want 403: %s",
					tc.method, tc.pattern, w.Code, w.Body.String())
			}

			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err == nil {
				for _, k := range []string{"token", "webhook_token", "signing_secret", "secret"} {
					if _, ok := body[k]; ok {
						t.Fatalf("%s leaked %q on a rejected request: %s", tc.name, k, w.Body.String())
					}
				}
			}
		})
	}
}
