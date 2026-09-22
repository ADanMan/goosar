package daemon

import (
	"net/http"
	"testing"
)

func TestDaemonUserAgentIsNamed(t *testing.T) {
	ua := daemonUserAgent(nil, "1.2.3", "macos")

	if ua == "" {
		t.Fatal("an empty User-Agent leaves Go's anonymous default in place")
	}
	if contains(ua, "Go-http-client") {
		t.Fatalf("the Go default must be replaced, not echoed: %q", ua)
	}
	if !contains(ua, "1.2.3") {
		t.Fatalf("the version identifies which build is calling: %q", ua)
	}
}

func TestDaemonUserAgentHonoursOverride(t *testing.T) {
	env := map[string]string{"GOOSAR_DAEMON_USER_AGENT": "CorporateMCP/1.0 (integration)"}
	if got := daemonUserAgent(env, "1.2.3", "macos"); got != "CorporateMCP/1.0 (integration)" {
		t.Fatalf("override ignored: %q", got)
	}
}

func TestDaemonUserAgentIgnoresBlankOverride(t *testing.T) {
	env := map[string]string{"GOOSAR_DAEMON_USER_AGENT": "   "}
	if got := daemonUserAgent(env, "1.2.3", "macos"); got == "" || got == "   " {
		t.Fatalf("a blank override must fall back to the default: %q", got)
	}
}

func TestIdentityHeadersSetUserAgent(t *testing.T) {
	c := &Client{platform: "daemon", version: "1.2.3", os: "macos"}
	req, err := http.NewRequest(http.MethodGet, "https://goosar.ru/api/daemon/workspaces", nil)
	if err != nil {
		t.Fatal(err)
	}

	c.setIdentityHeaders(req)

	if req.Header.Get("User-Agent") == "" {
		t.Fatal("every daemon request must identify itself")
	}
}
