package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/cli"
)

func TestProbeServerNamesTheReason(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("GOOSAR_HTTP_TIMEOUT", "")

	t.Run("healthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
		defer srv.Close()
		ok, reason := probeServer(srv.URL)
		if !ok || reason != "" {
			t.Fatalf("ok=%v reason=%q", ok, reason)
		}
	})

	t.Run("untrusted certificate names --ca-file", func(t *testing.T) {
		t.Cleanup(func() { _ = cli.ConfigureCA("") })
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
		defer srv.Close()
		ok, reason := probeServer(srv.URL)
		if ok || !strings.Contains(reason, "--ca-file") {
			t.Fatalf("ok=%v reason=%q, want the --ca-file hint", ok, reason)
		}
	})

	t.Run("connection refused names the port", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		l.Close()
		ok, reason := probeServer("http://" + addr)
		if ok || !strings.Contains(reason, "Could not connect") {
			t.Fatalf("ok=%v reason=%q", ok, reason)
		}
	})

	t.Run("unresolvable host names DNS", func(t *testing.T) {
		ok, reason := probeServer("http://stand.nope.invalid")
		if ok || !strings.Contains(reason, "resolve") {
			t.Fatalf("ok=%v reason=%q", ok, reason)
		}
	})

	t.Run("5xx says the server is up but /health is not", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
		defer srv.Close()
		ok, reason := probeServer(srv.URL)
		if ok || !strings.Contains(reason, "503") || !strings.Contains(reason, "/health") {
			t.Fatalf("ok=%v reason=%q", ok, reason)
		}
	})

	t.Run("auth-gated /health is named as such", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }))
		defer srv.Close()
		ok, reason := probeServer(srv.URL)
		if ok || !strings.Contains(reason, "401") || !strings.Contains(reason, "proxy") {
			t.Fatalf("ok=%v reason=%q", ok, reason)
		}
	})

	t.Run("timeout honours GOOSAR_HTTP_TIMEOUT", func(t *testing.T) {
		t.Setenv("GOOSAR_HTTP_TIMEOUT", "200ms")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(1500 * time.Millisecond)
			w.WriteHeader(200)
		}))
		defer srv.Close()
		start := time.Now()
		ok, reason := probeServer(srv.URL)
		if ok || !strings.Contains(reason, "timed out") {
			t.Fatalf("ok=%v reason=%q", ok, reason)
		}
		if time.Since(start) > time.Second {
			t.Fatalf("probe took %s, want ~200ms", time.Since(start))
		}
	})
}

func TestRequireHTTPScheme(t *testing.T) {
	if err := requireHTTPScheme("api.internal.co"); err == nil || !strings.Contains(err.Error(), "https://") {
		t.Fatalf("want a scheme hint, got %v", err)
	}
	for _, ok := range []string{"http://x", "https://x/"} {
		if err := requireHTTPScheme(ok); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
}

func TestMergeSetupConfigKeepsTheProfile(t *testing.T) {
	existing := cli.CLIConfig{
		ServerURL: "https://goosar.ru", AppURL: "https://goosar.ru",
		Token: "gsl_keep", WorkspaceID: "ws-1", DeviceName: "laptop", CAFile: "/old.crt",
	}
	same := mergeSetupConfig(existing, "https://goosar.ru", "https://goosar.ru", "")
	if same.Token != "gsl_keep" || same.WorkspaceID != "ws-1" || same.DeviceName != "laptop" || same.CAFile != "/old.crt" {
		t.Fatalf("same server: profile not kept: %+v", same)
	}
	other := mergeSetupConfig(existing, "https://stand.local", "https://stand.local", "/stand.crt")
	if other.Token != "" || other.WorkspaceID != "" {
		t.Fatalf("other server: a token minted for goosar.ru must not be kept: %+v", other)
	}
	if other.DeviceName != "laptop" || other.CAFile != "/stand.crt" {
		t.Fatalf("other server: daemon knobs kept, ca_file replaced: %+v", other)
	}
}

func TestConfirmOverwriteFrom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := cli.SaveCLIConfig(cli.CLIConfig{ServerURL: "https://old"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := confirmOverwriteFrom(strings.NewReader("y\n"), "", "https://new", "https://new", false); err != nil || !ok {
		t.Fatalf("y: ok=%v err=%v", ok, err)
	}

	if ok, err := confirmOverwriteFrom(strings.NewReader("y"), "", "https://new", "https://new", false); err != nil || !ok {
		t.Fatalf("y without newline: ok=%v err=%v", ok, err)
	}
	if ok, err := confirmOverwriteFrom(strings.NewReader("n\n"), "", "https://new", "https://new", false); err != nil || ok {
		t.Fatalf("n: ok=%v err=%v", ok, err)
	}

	if _, err := confirmOverwriteFrom(strings.NewReader(""), "", "https://new", "https://new", false); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("closed stdin: want an error naming --yes, got %v", err)
	}
	if ok, err := confirmOverwriteFrom(strings.NewReader(""), "", "https://new", "https://new", true); err != nil || !ok {
		t.Fatalf("--yes: ok=%v err=%v", ok, err)
	}
}

func TestProbeTimeoutIgnoresAnInvalidValue(t *testing.T) {
	t.Setenv("GOOSAR_HTTP_TIMEOUT", "banana")
	if got := cli.ProbeTimeout(); got != 5*time.Second {
		t.Fatalf("invalid value: got %s, want the probe's own 5s", got)
	}
	t.Setenv("GOOSAR_HTTP_TIMEOUT", "2")
	if got := cli.ProbeTimeout(); got != 2*time.Second {
		t.Fatalf("plain seconds: got %s", got)
	}
}
