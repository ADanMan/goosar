package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetConfig_DeploymentHostsField(t *testing.T) {
	getConfig := func(t *testing.T) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d", w.Code)
		}
		var cfg map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	clearEnv := func(t *testing.T) {
		t.Helper()
		for _, env := range []string{
			"GOOSAR_DEPLOYMENT_JIRA_URL",
			"GOOSAR_DEPLOYMENT_CONFLUENCE_URL",
			"GOOSAR_DEPLOYMENT_EWS_URL",
			"GOOSAR_DEPLOYMENT_MAIL_DOMAIN",
			"GOOSAR_DEPLOYMENT_LLM_API_BASE",
			"GOOSAR_DEPLOYMENT_LLM_MODEL",
		} {
			t.Setenv(env, "")
		}
	}

	t.Run("omits the field when nothing is configured", func(t *testing.T) {
		clearEnv(t)
		if _, present := getConfig(t)["deployment_hosts"]; present {
			t.Fatal("deployment_hosts must be omitted when no address is set")
		}
	})

	t.Run("publishes exactly the addresses the operator set", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("GOOSAR_DEPLOYMENT_JIRA_URL", "https://jira.example.test")
		t.Setenv("GOOSAR_DEPLOYMENT_LLM_API_BASE", "  https://llm.example.test/v1  ")

		got, present := getConfig(t)["deployment_hosts"]
		if !present {
			t.Fatal("deployment_hosts must be present once an address is set")
		}
		hosts, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("deployment_hosts = %#v, want an object", got)
		}
		if hosts["jiraUrl"] != "https://jira.example.test" {
			t.Fatalf("jiraUrl = %#v", hosts["jiraUrl"])
		}

		if hosts["llmApiBase"] != "https://llm.example.test/v1" {
			t.Fatalf("llmApiBase = %#v", hosts["llmApiBase"])
		}

		if _, present := hosts["confluenceUrl"]; present {
			t.Fatalf("confluenceUrl must be absent, got %#v", hosts["confluenceUrl"])
		}
	})

	t.Run("treats a blank value as unset", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("GOOSAR_DEPLOYMENT_MAIL_DOMAIN", "   ")
		if _, present := getConfig(t)["deployment_hosts"]; present {
			t.Fatal("a blank address must not produce a deployment_hosts field")
		}
	})

	t.Run("carries only address fields", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("GOOSAR_DEPLOYMENT_JIRA_URL", "https://jira.example.test")
		t.Setenv("GOOSAR_DEPLOYMENT_CONFLUENCE_URL", "https://wiki.example.test")
		t.Setenv("GOOSAR_DEPLOYMENT_EWS_URL", "https://mail.example.test/EWS/Exchange.asmx")
		t.Setenv("GOOSAR_DEPLOYMENT_MAIL_DOMAIN", "example.test")
		t.Setenv("GOOSAR_DEPLOYMENT_LLM_API_BASE", "https://llm.example.test/v1")
		t.Setenv("GOOSAR_DEPLOYMENT_LLM_MODEL", "openai/coding-medium")

		hosts, ok := getConfig(t)["deployment_hosts"].(map[string]any)
		if !ok {
			t.Fatal("deployment_hosts must be an object")
		}
		allowed := map[string]bool{
			"jiraUrl": true, "confluenceUrl": true, "ewsUrl": true,
			"mailDomain": true, "llmApiBase": true, "llmModel": true,
		}
		for key := range hosts {
			if !allowed[key] {
				t.Fatalf("unexpected field %q in deployment_hosts", key)
			}
		}
		if len(hosts) != len(allowed) {
			t.Fatalf("deployment_hosts = %#v, want all %d address fields", hosts, len(allowed))
		}
	})
}
