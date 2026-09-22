package preflight

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func probeOK(_ context.Context) ServerProbe {
	return ServerProbe{OK: true, Version: "v0.11.0"}
}

func daemonRunning(_ context.Context) DaemonProbe { return DaemonProbe{Status: DaemonRunning} }

func kitFor(total, forPlatform int, platforms ...string) func(context.Context) (KitProbe, error) {
	return func(_ context.Context) (KitProbe, error) {
		return KitProbe{Total: total, ForPlatform: forPlatform, Platforms: platforms}, nil
	}
}

func healthy() ServerOptions {
	return ServerOptions{
		ConfigPath:        "/home/u/.goosar/config.json",
		Profile:           "default",
		ServerURL:         "https://goosar.example.com",
		CAFile:            "",
		Platform:          "darwin-arm64",
		ProbeServer:       probeOK,
		DaemonHealth:      daemonRunning,
		KitManifest:       kitFor(12, 5, "darwin-arm64", "linux-x64"),
		InstalledPackages: func() int { return 5 },
	}
}

func rowByID(t *testing.T, rows []Result, id string) Result {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no row %q in %v", id, rows)
	return Result{}
}

func TestRunServer_HealthyStand(t *testing.T) {
	rows := RunServer(context.Background(), healthy())
	want := []string{"config", "server", "certificate", "daemon", "kit-platform", "llm"}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d: %+v", len(rows), len(want), rows)
	}
	for i, id := range want {
		if rows[i].ID != id {
			t.Errorf("row %d = %q, want %q", i, rows[i].ID, id)
		}
		if id == "llm" {

			if rows[i].Status != StatusSkipped {
				t.Errorf("row %q status = %q, want skipped (no probe wired)", id, rows[i].Status)
			}
			continue
		}
		if rows[i].Status != StatusOK {
			t.Errorf("row %q status = %q, want ok (%s)", id, rows[i].Status, rows[i].Detail)
		}
		if rows[i].Message == "" {
			t.Errorf("row %q has no message", id)
		}
	}
	if s := rowByID(t, rows, "server"); !strings.Contains(s.Detail, "v0.11.0") {
		t.Errorf("server row should carry the server version, got %q", s.Detail)
	}
	if k := rowByID(t, rows, "kit-platform"); !strings.Contains(k.Detail, "5") {
		t.Errorf("kit row should say how many packages exist for this platform, got %q", k.Detail)
	}
}

func TestRunServer_LLMRowFollowsHealthVerdict(t *testing.T) {
	cases := []struct {
		name       string
		llmHealth  func(context.Context) (LLMHealthProbe, error)
		wantStatus string
	}{
		{"ok", func(context.Context) (LLMHealthProbe, error) {
			return LLMHealthProbe{Status: "ok", LatencyMS: 42}, nil
		}, StatusOK},
		{"unconfigured", func(context.Context) (LLMHealthProbe, error) {
			return LLMHealthProbe{Status: "unconfigured"}, nil
		}, StatusSkipped},
		{"auth_rejected", func(context.Context) (LLMHealthProbe, error) {
			return LLMHealthProbe{Status: "auth_rejected"}, nil
		}, StatusMissing},
		{"degraded", func(context.Context) (LLMHealthProbe, error) {
			return LLMHealthProbe{Status: "degraded"}, nil
		}, StatusMissing},
		{"probe error", func(context.Context) (LLMHealthProbe, error) {
			return LLMHealthProbe{}, errors.New("timeout")
		}, StatusSkipped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := healthy()
			opts.LLMHealth = tc.llmHealth
			rows := RunServer(context.Background(), opts)
			if l := rowByID(t, rows, "llm"); l.Status != tc.wantStatus {
				t.Errorf("llm row status = %q, want %q (%+v)", l.Status, tc.wantStatus, l)
			}
		})
	}
}

func TestRunServer_LLMRowSkippedWhenServerDown(t *testing.T) {
	opts := healthy()
	opts.ProbeServer = func(context.Context) ServerProbe { return ServerProbe{OK: false, Reason: "down"} }
	opts.LLMHealth = func(context.Context) (LLMHealthProbe, error) {
		t.Fatal("LLMHealth should not be called when the server itself did not respond")
		return LLMHealthProbe{}, nil
	}
	rows := RunServer(context.Background(), opts)
	if l := rowByID(t, rows, "llm"); l.Status != StatusSkipped {
		t.Errorf("llm row = %+v, want skipped when server is down", l)
	}
}

func TestRunServer_NoConfigSkipsEverythingDownstream(t *testing.T) {
	opts := healthy()
	opts.ServerURL = ""
	rows := RunServer(context.Background(), opts)
	if c := rowByID(t, rows, "config"); c.Status != StatusMissing || !strings.Contains(c.Fix, "goosar setup") {
		t.Errorf("config row = %+v, want missing with a `goosar setup` fix", c)
	}
	for _, id := range []string{"server", "certificate", "daemon", "kit-platform", "llm"} {
		if r := rowByID(t, rows, id); r.Status != StatusSkipped {
			t.Errorf("row %q = %q, want skipped when there is no server configured", id, r.Status)
		}
	}
}

func TestRunServer_ServerDownCarriesTheProbeReason(t *testing.T) {
	opts := healthy()
	opts.ProbeServer = func(context.Context) ServerProbe {
		return ServerProbe{OK: false, Reason: "connection refused on port 443"}
	}
	rows := RunServer(context.Background(), opts)
	s := rowByID(t, rows, "server")
	if s.Status != StatusMissing || !strings.Contains(s.Message, "connection refused on port 443") {
		t.Errorf("server row = %+v, want missing with the probe's own reason", s)
	}

	for _, id := range []string{"certificate", "kit-platform"} {
		if r := rowByID(t, rows, id); r.Status != StatusSkipped {
			t.Errorf("row %q = %q, want skipped when the server is down", id, r.Status)
		}
	}
}

func TestRunServer_UntrustedCertificatePointsAtCAFile(t *testing.T) {
	opts := healthy()
	opts.ProbeServer = func(context.Context) ServerProbe {
		return ServerProbe{OK: false, Reason: "x509: certificate signed by unknown authority", TLSUntrusted: true}
	}
	rows := RunServer(context.Background(), opts)
	c := rowByID(t, rows, "certificate")
	if c.Status != StatusMissing || !strings.Contains(c.Fix, "--ca-file") {
		t.Errorf("certificate row = %+v, want missing with a --ca-file fix", c)
	}
}

func TestRunServer_CertificateRowNamesTheTrustSource(t *testing.T) {
	cases := []struct {
		name, url, ca, want string
	}{
		{"plain http", "http://10.0.0.5:3001", "", "HTTP"},
		{"system trust", "https://goosar.example.com", "", "систем"},
		{"ca file", "https://stand.local", "/etc/stand-root-ca.crt", "/etc/stand-root-ca.crt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := healthy()
			opts.ServerURL, opts.CAFile = tc.url, tc.ca
			c := rowByID(t, RunServer(context.Background(), opts), "certificate")
			if !strings.Contains(c.Detail, tc.want) {
				t.Errorf("detail = %q, want it to mention %q", c.Detail, tc.want)
			}
			if tc.name == "plain http" && c.Status != StatusSkipped {
				t.Errorf("plain HTTP has no certificate to check; status = %q", c.Status)
			}
		})
	}
}

func TestRunServer_DaemonStatesAreDistinct(t *testing.T) {
	cases := map[string]string{
		DaemonRunning:     StatusOK,
		DaemonStarting:    StatusOK,
		DaemonStopped:     StatusMissing,
		DaemonUnreachable: StatusMissing,
		DaemonInvalid:     StatusMissing,
	}
	seen := map[string]bool{}
	for status, want := range cases {
		opts := healthy()
		opts.DaemonHealth = func(context.Context) DaemonProbe { return DaemonProbe{Status: status, Detail: "port 19514"} }
		d := rowByID(t, RunServer(context.Background(), opts), "daemon")
		if d.Status != want {
			t.Errorf("daemon %q → %q, want %q", status, d.Status, want)
		}

		if seen[d.Message] {
			t.Errorf("daemon state %q reuses another state's message: %q", status, d.Message)
		}
		seen[d.Message] = true
	}
}

func TestRunServer_KitRowTellsPlatformGapFromEmptyStore(t *testing.T) {
	gap := healthy()
	gap.Platform = "win-x64"
	gap.KitManifest = kitFor(12, 0, "darwin-arm64", "linux-x64")
	k := rowByID(t, RunServer(context.Background(), gap), "kit-platform")
	if k.Status != StatusMissing || !strings.Contains(k.Message, "win-x64") || !strings.Contains(k.Message, "darwin-arm64") {
		t.Errorf("platform gap row = %+v, want missing naming both this platform and the ones that exist", k)
	}

	empty := healthy()
	empty.KitManifest = kitFor(0, 0)
	k = rowByID(t, RunServer(context.Background(), empty), "kit-platform")
	if k.Status != StatusSkipped {
		t.Errorf("empty store row = %+v, want skipped (nothing published is not a client defect)", k)
	}

	failing := healthy()
	failing.KitManifest = func(context.Context) (KitProbe, error) { return KitProbe{}, errors.New("HTTP 503") }
	k = rowByID(t, RunServer(context.Background(), failing), "kit-platform")
	if k.Status != StatusMissing || !strings.Contains(k.Message, "HTTP 503") {
		t.Errorf("manifest error row = %+v, want missing carrying the error", k)
	}
}

func TestRunServer_JSONFieldsStayCompatible(t *testing.T) {

	rows := RunServer(context.Background(), healthy())
	for _, r := range rows {
		if r.Name == "" || r.Status == "" {
			t.Errorf("row %q is missing name/status: %+v", r.ID, r)
		}
	}
}
