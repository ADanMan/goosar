package main

import (
	"net/http"
	"testing"
)

func TestSelfServiceReads_RejectMachineActorOnRealRouter(t *testing.T) {
	if testPool == nil || testServer == nil {
		t.Skip("database not available")
	}

	taskToken := mintAgentTaskTokenFixture(t)

	for _, path := range []string{"/api/auth/mfa", "/api/auth/sessions"} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, testServer.URL+path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+taskToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("machine actor on %s: status = %d, want 403", path, resp.StatusCode)
			}
		})
	}
}
