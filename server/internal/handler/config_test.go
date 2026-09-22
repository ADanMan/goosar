package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

func TestGetConfigReportsCdnSignedMode(t *testing.T) {
	origStorage := testHandler.Storage
	origSigner := testHandler.CFSigner
	testHandler.Storage = &mockStorage{}
	defer func() {
		testHandler.Storage = origStorage
		testHandler.CFSigner = origSigner
	}()

	fetch := func() AppConfig {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var cfg AppConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	testHandler.CFSigner = nil
	if cfg := fetch(); cfg.CdnSigned {
		t.Fatalf("cdn_signed: want false without a CloudFront signer, got true")
	}

	testHandler.CFSigner = &auth.CloudFrontSigner{}
	cfg := fetch()
	if !cfg.CdnSigned {
		t.Fatalf("cdn_signed: want true with a CloudFront signer, got false")
	}
	if cfg.CdnDomain != "cdn.example.com" {
		t.Fatalf("cdn_domain: want cdn.example.com alongside cdn_signed, got %q", cfg.CdnDomain)
	}
}

func TestGetConfigIncludesRuntimeAuthConfig(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	t.Setenv("ALLOW_SIGNUP", "false")
	t.Setenv("POSTHOG_API_KEY", "phc_test")
	t.Setenv("POSTHOG_HOST", "https://eu.i.posthog.com")

	t.Setenv("ANALYTICS_FRONTEND_ENABLED", "true")
	t.Setenv("GOOSAR_PUBLIC_URL", "https://api.example.com/")
	t.Setenv("GOOSAR_APP_URL", "https://app.example.com/")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	if cfg.CdnDomain != "cdn.example.com" {
		t.Fatalf("cdn_domain: want cdn.example.com, got %q", cfg.CdnDomain)
	}
	if cfg.AllowSignup {
		t.Fatalf("allow_signup: want false, got true")
	}
	if cfg.PosthogKey != "phc_test" {
		t.Fatalf("posthog_key: want phc_test, got %q", cfg.PosthogKey)
	}
	if cfg.PosthogHost != "https://eu.i.posthog.com" {
		t.Fatalf("posthog_host: want https://eu.i.posthog.com, got %q", cfg.PosthogHost)
	}
	if cfg.AnalyticsEnvironment != "dev" {
		t.Fatalf("analytics_environment: want dev, got %q", cfg.AnalyticsEnvironment)
	}
	if cfg.WorkspaceCreationDisabled {
		t.Fatalf("workspace_creation_disabled: want false by default, got true")
	}
	if cfg.DaemonServerURL != "https://api.example.com" {
		t.Fatalf("daemon_server_url: want https://api.example.com, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "https://app.example.com" {
		t.Fatalf("daemon_app_url: want https://app.example.com, got %q", cfg.DaemonAppURL)
	}
}

func TestGetConfigWithholdsPosthogKeyWithoutOptIn(t *testing.T) {
	fetch := func() AppConfig {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var cfg AppConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	t.Setenv("POSTHOG_API_KEY", "phc_test")
	t.Setenv("POSTHOG_HOST", "https://eu.i.posthog.com")

	t.Run("default off", func(t *testing.T) {
		t.Setenv("ANALYTICS_FRONTEND_ENABLED", "")
		cfg := fetch()
		if cfg.PosthogKey != "" {
			t.Fatalf("posthog_key must be withheld without ANALYTICS_FRONTEND_ENABLED, got %q", cfg.PosthogKey)
		}
		if cfg.PosthogHost != "" {
			t.Fatalf("posthog_host must be withheld without ANALYTICS_FRONTEND_ENABLED, got %q", cfg.PosthogHost)
		}
	})

	t.Run("arbitrary value is not an opt-in", func(t *testing.T) {
		t.Setenv("ANALYTICS_FRONTEND_ENABLED", "yes-please")
		if cfg := fetch(); cfg.PosthogKey != "" {
			t.Fatalf("posthog_key must be withheld for non-true opt-in values, got %q", cfg.PosthogKey)
		}
	})

	t.Run("explicit opt-in hands the key over", func(t *testing.T) {
		t.Setenv("ANALYTICS_FRONTEND_ENABLED", "true")
		cfg := fetch()
		if cfg.PosthogKey != "phc_test" {
			t.Fatalf("posthog_key: want phc_test, got %q", cfg.PosthogKey)
		}
		if cfg.PosthogHost != "https://eu.i.posthog.com" {
			t.Fatalf("posthog_host: want https://eu.i.posthog.com, got %q", cfg.PosthogHost)
		}
	})

	t.Run("kill switch wins over opt-in", func(t *testing.T) {
		t.Setenv("ANALYTICS_FRONTEND_ENABLED", "true")
		t.Setenv("ANALYTICS_DISABLED", "true")
		if cfg := fetch(); cfg.PosthogKey != "" {
			t.Fatalf("posthog_key must be withheld when ANALYTICS_DISABLED is set, got %q", cfg.PosthogKey)
		}
	})
}

func TestGetConfigHonorsVCSIntegrationSwitch(t *testing.T) {
	origCfg := testHandler.cfg
	t.Cleanup(func() { testHandler.cfg = origCfg })

	fetch := func() map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	testHandler.cfg.VCSIntegrationEnabled = false
	if _, ok := fetch()["vcs_integration_available"]; ok {
		t.Fatal("vcs_integration_available must be omitted when the integration is disabled")
	}

	testHandler.cfg.VCSIntegrationEnabled = true
	raw, ok := fetch()["vcs_integration_available"]
	if !ok {
		t.Fatal("vcs_integration_available must be present when the integration is enabled")
	}
	if string(raw) != "true" {
		t.Fatalf("vcs_integration_available: want true, got %s", raw)
	}
}

func TestGetConfigUsesAppURLForSameOriginDaemonSetup(t *testing.T) {
	t.Setenv("GOOSAR_PUBLIC_URL", "")
	t.Setenv("GOOSAR_APP_URL", "https://goosar.internal.example/")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "https://goosar.internal.example" {
		t.Fatalf("daemon_server_url: want same-origin URL, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "https://goosar.internal.example" {
		t.Fatalf("daemon_app_url: want app URL, got %q", cfg.DaemonAppURL)
	}
}

func TestGetConfigUsesFrontendOriginForSameOriginDaemonSetup(t *testing.T) {
	t.Setenv("GOOSAR_PUBLIC_URL", "")
	t.Setenv("GOOSAR_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "https://goosar.internal.example/")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "https://goosar.internal.example" {
		t.Fatalf("daemon_server_url: want same-origin URL, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "https://goosar.internal.example" {
		t.Fatalf("daemon_app_url: want frontend origin, got %q", cfg.DaemonAppURL)
	}
}

func TestGetConfigOmitsOfficialCloudDaemonSetup(t *testing.T) {
	t.Setenv("GOOSAR_PUBLIC_URL", "https://goosar.ru")
	t.Setenv("GOOSAR_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "https://goosar.ru")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "" {
		t.Fatalf("daemon_server_url: want omitted for cloud, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "" {
		t.Fatalf("daemon_app_url: want omitted for cloud, got %q", cfg.DaemonAppURL)
	}
}

func TestGetConfigOmitsCloudDaemonSetupWithoutPublicURL(t *testing.T) {
	t.Setenv("GOOSAR_PUBLIC_URL", "")
	t.Setenv("GOOSAR_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "https://goosar.ru")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "" {
		t.Fatalf("daemon_server_url: want omitted for official cloud, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "" {
		t.Fatalf("daemon_app_url: want omitted for official cloud, got %q", cfg.DaemonAppURL)
	}
}

func TestGetConfigOmitsCloudDaemonSetupForConfiguredAppURL(t *testing.T) {
	t.Setenv("GOOSAR_PUBLIC_URL", "")
	t.Setenv("GOOSAR_APP_URL", "https://goosar.ru")
	t.Setenv("FRONTEND_ORIGIN", "")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "" {
		t.Fatalf("daemon_server_url: want omitted for official cloud, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "" {
		t.Fatalf("daemon_app_url: want omitted for official cloud, got %q", cfg.DaemonAppURL)
	}
}

func TestURLHostEqualsCanonicalizesCommonHostForms(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "full URL", raw: "https://goosar.ru", want: true},
		{name: "bare host", raw: "goosar.ru", want: true},
		{name: "host port", raw: "goosar.ru:8080", want: true},
		{name: "trailing dot", raw: "https://goosar.ru.", want: true},
		{name: "different host", raw: "https://evil.example", want: false},
		{name: "empty", raw: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := urlHostEquals(tt.raw, "goosar.ru"); got != tt.want {
				t.Fatalf("urlHostEquals(%q): want %v, got %v", tt.raw, tt.want, got)
			}
		})
	}
}

func TestGetConfigExposesWorkspaceCreationDisabled(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	t.Setenv("DISABLE_WORKSPACE_CREATION", "true")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !cfg.WorkspaceCreationDisabled {
		t.Fatalf("workspace_creation_disabled: want true with env on, got false (body=%s)", w.Body.String())
	}
}

func TestGetConfigExposesServerVersion(t *testing.T) {
	origCfg := testHandler.cfg
	defer func() { testHandler.cfg = origCfg }()

	t.Setenv("GOOSAR_APP_URL", "https://goosar.self-hosted.example")
	t.Setenv("FRONTEND_ORIGIN", "")

	testHandler.cfg.ServerVersion = ""
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.ServerVersion != "" {
		t.Fatalf("server_version: want empty for dev build, got %q", cfg.ServerVersion)
	}

	testHandler.cfg.ServerVersion = "1.2.3"
	w = httptest.NewRecorder()
	testHandler.GetConfig(w, req)
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.ServerVersion != "1.2.3" {
		t.Fatalf("server_version: want 1.2.3, got %q", cfg.ServerVersion)
	}
}

func TestGetConfigOmitsServerVersionOnOfficialCloud(t *testing.T) {
	origCfg := testHandler.cfg
	defer func() { testHandler.cfg = origCfg }()
	testHandler.cfg.ServerVersion = "1.2.3"

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)

	t.Setenv("GOOSAR_APP_URL", "https://goosar.ru")
	t.Setenv("FRONTEND_ORIGIN", "")
	w := httptest.NewRecorder()
	testHandler.GetConfig(w, req)
	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.ServerVersion != "" {
		t.Fatalf("server_version: want omitted on official cloud, got %q", cfg.ServerVersion)
	}

	t.Setenv("GOOSAR_APP_URL", "https://goosar.self-hosted.example")
	w = httptest.NewRecorder()
	testHandler.GetConfig(w, req)
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.ServerVersion != "1.2.3" {
		t.Fatalf("server_version: want 1.2.3 on self-hosted, got %q", cfg.ServerVersion)
	}
}

func TestGetConfigExposesFrontendFeatureFlags(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig default flags: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode default config: %v", err)
	}
	if cfg.FeatureFlags["composio_mcp_apps"] {
		t.Fatalf("composio_mcp_apps: want false by default, got true")
	}
	if !cfg.FeatureFlags["agents_skill_toggles"] {
		t.Fatalf("agents_skill_toggles: want true for installed v0.4.0 clients, got false")
	}

	withComposioMCPAppsFlag(t, h, true)
	w = httptest.NewRecorder()
	h.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig enabled flags: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode enabled config: %v", err)
	}
	if !cfg.FeatureFlags["composio_mcp_apps"] {
		t.Fatalf("composio_mcp_apps: want true with flag enabled, got false")
	}
}

func TestGetConfigExposesDeliveryProfile(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	origCfg := testHandler.cfg
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
	})

	fetch := func() map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	testHandler.cfg.DeliveryProfile = deliveryprofile.Cloud
	if _, ok := fetch()["delivery_profile"]; ok {
		t.Fatal("delivery_profile must be omitted on the cloud profile (byte-for-byte unchanged payload)")
	}

	testHandler.cfg.DeliveryProfile = ""
	if _, ok := fetch()["delivery_profile"]; ok {
		t.Fatal("delivery_profile must be omitted for the zero-value profile")
	}

	testHandler.cfg.DeliveryProfile = deliveryprofile.Perimeter
	raw, ok := fetch()["delivery_profile"]
	if !ok {
		t.Fatal("delivery_profile must be present on the perimeter profile")
	}
	if string(raw) != `"perimeter"` {
		t.Fatalf("delivery_profile: want \"perimeter\", got %s", raw)
	}
}

func TestInitiateUpdateRefusedOnPerimeterProfile(t *testing.T) {
	origCfg := testHandler.cfg
	t.Cleanup(func() { testHandler.cfg = origCfg })
	testHandler.cfg.DeliveryProfile = deliveryprofile.Perimeter

	req := httptest.NewRequest(http.MethodPost, "/api/runtimes/00000000-0000-0000-0000-000000000001/update", nil)
	w := httptest.NewRecorder()
	testHandler.InitiateUpdate(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("InitiateUpdate on perimeter: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetConfigExternalImagePolicy(t *testing.T) {
	fetch := func() AppConfig {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var cfg AppConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "")
	t.Setenv("GOOSAR_IMAGE_HOSTS", "cdn.example.com")
	if cfg := fetch(); cfg.ExternalImages != "" || cfg.ImageHosts != nil {
		t.Fatalf("default mode: want omitted policy fields, got mode=%q hosts=%v", cfg.ExternalImages, cfg.ImageHosts)
	}

	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "block")
	if cfg := fetch(); cfg.ExternalImages != "block" || cfg.ImageHosts != nil {
		t.Fatalf("block mode: want mode=block without hosts, got mode=%q hosts=%v", cfg.ExternalImages, cfg.ImageHosts)
	}

	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "allowlist")
	t.Setenv("GOOSAR_IMAGE_HOSTS", " cdn.example.com , evil.com; script-src * ")
	cfg := fetch()
	if cfg.ExternalImages != "allowlist" {
		t.Fatalf("allowlist mode: want mode=allowlist, got %q", cfg.ExternalImages)
	}
	if len(cfg.ImageHosts) != 1 || cfg.ImageHosts[0] != "cdn.example.com" {
		t.Fatalf("allowlist hosts: want [cdn.example.com], got %v", cfg.ImageHosts)
	}

	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "blok")
	if cfg := fetch(); cfg.ExternalImages != "block" {
		t.Fatalf("invalid mode: want block, got %q", cfg.ExternalImages)
	}
}
