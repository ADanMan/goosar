package main

import (
	"io"
	"net/http"
	"testing"
)

func TestComposioCallbackIsPublic_NoCookieNot401(t *testing.T) {

	resp, err := http.Get(testServer.URL + "/api/integrations/composio/callback?state=bogus&status=success&connected_account_id=ca_x")
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("callback returned 401 without a session — it is still behind the Auth group (regression of MUL-3843). body=%s", body)
	}
}

func TestComposioNonCallbackEndpointsStayGated(t *testing.T) {
	gated := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/integrations/composio/connect/init"},
		{http.MethodGet, "/api/integrations/composio/toolkits"},
		{http.MethodGet, "/api/integrations/composio/connections"},
		{http.MethodDelete, "/api/integrations/composio/connections/11111111-1111-1111-1111-111111111111"},
	}
	for _, tc := range gated {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, testServer.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401 without a session, got %d — endpoint is no longer auth-gated", resp.StatusCode)
			}
		})
	}
}
