package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/preflight"
)

func fakeGoosarServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"server_version":"v1.0.0","min_daemon_version":"0.2.21"}`)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ready"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func bundleFixture(t *testing.T, serverURL string) supportBundleOptions {
	t.Helper()
	daemonDir := t.TempDir()
	logBody := strings.Join([]string{
		"level=INFO msg=\"daemon started\"",
		"level=ERROR msg=\"push failed\" GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn",
		"level=INFO msg=\"оператор +7 912 345-67-89 просил перезапуск\"",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(daemonDir, "daemon.log"), []byte(logBody), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(daemonDir, "daemon.err.log"), []byte("panic: boom\n"), 0o600); err != nil {
		t.Fatalf("write daemon.err.log: %v", err)
	}
	return supportBundleOptions{
		ServerURL: serverURL,
		DaemonDir: daemonDir,
		LogLines:  50,
		Now:       time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		HTTP:      &http.Client{Timeout: 5 * time.Second},
		Preflight: func(context.Context) preflight.Report {
			return preflight.Report{OK: true, Results: []preflight.Result{{Name: "git", Status: preflight.StatusOK, Detail: "git version 2.44.0"}}}
		},
	}
}

func readBundle(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	files := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		files[hdr.Name] = string(body)
	}
	return files
}

func TestSupportBundle_WritesEveryOperatorSection(t *testing.T) {
	srv := fakeGoosarServer(t)
	opts := bundleFixture(t, srv.URL)

	var buf bytes.Buffer
	if err := writeSupportBundle(context.Background(), &buf, opts); err != nil {
		t.Fatalf("writeSupportBundle: %v", err)
	}
	files := readBundle(t, buf.Bytes())

	for _, want := range []string{"manifest.txt", "versions.txt", "doctor.json", "daemon.log.txt", "daemon.err.log.txt", "config.txt", "os.txt", "network.txt"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("bundle is missing %s; has %v", want, keysOf(files))
		}
	}

	if !strings.Contains(files["versions.txt"], "min_daemon_version") {
		t.Fatalf("versions.txt does not carry the server's min_daemon_version:\n%s", files["versions.txt"])
	}
	if !strings.Contains(files["doctor.json"], "\"git\"") {
		t.Fatalf("doctor.json does not carry the preflight report:\n%s", files["doctor.json"])
	}
	if !strings.Contains(files["network.txt"], "/healthz") {
		t.Fatalf("network.txt does not record server reachability:\n%s", files["network.txt"])
	}
	if !strings.Contains(files["os.txt"], runtime.GOOS) {
		t.Fatalf("os.txt does not record OS facts:\n%s", files["os.txt"])
	}
}

func TestSupportBundle_ManifestListsEveryFileWithChecksum(t *testing.T) {
	srv := fakeGoosarServer(t)
	var buf bytes.Buffer
	if err := writeSupportBundle(context.Background(), &buf, bundleFixture(t, srv.URL)); err != nil {
		t.Fatalf("writeSupportBundle: %v", err)
	}
	files := readBundle(t, buf.Bytes())
	manifest := files["manifest.txt"]

	for name := range files {
		if name == "manifest.txt" {
			continue
		}
		if !strings.Contains(manifest, name) {
			t.Fatalf("manifest does not list %s:\n%s", name, manifest)
		}
	}
	if strings.Count(manifest, "sha256=") < len(files)-1 {
		t.Fatalf("manifest is missing checksums:\n%s", manifest)
	}
}

func TestSupportBundle_EveryTextFileIsRedacted(t *testing.T) {
	t.Setenv("GOOSAR_API_TOKEN", "s3cr3t-support-bundle-value")
	srv := fakeGoosarServer(t)
	var buf bytes.Buffer
	if err := writeSupportBundle(context.Background(), &buf, bundleFixture(t, srv.URL)); err != nil {
		t.Fatalf("writeSupportBundle: %v", err)
	}
	whole := strings.Join(valuesOf(readBundle(t, buf.Bytes())), "\n")

	for _, leak := range []string{"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ", "s3cr3t-support-bundle-value", "912 345-67-89"} {
		if strings.Contains(whole, leak) {
			t.Fatalf("%q reached the bundle unredacted", leak)
		}
	}
	if !strings.Contains(whole, "[REDACTED") {
		t.Fatalf("nothing was redacted at all — the pipeline is not wired")
	}
}

func TestSupportBundle_ServerSectionOnlyOnTheServerHost(t *testing.T) {
	srv := fakeGoosarServer(t)

	opts := bundleFixture(t, srv.URL)
	var buf bytes.Buffer
	if err := writeSupportBundle(context.Background(), &buf, opts); err != nil {
		t.Fatalf("writeSupportBundle: %v", err)
	}
	if _, ok := readBundle(t, buf.Bytes())["server.txt"]; ok {
		t.Fatal("server.txt was collected without DATABASE_URL")
	}

	opts.DatabaseURL = "postgres://nobody:nobody@127.0.0.1:1/nonexistent?sslmode=disable&connect_timeout=1"
	buf.Reset()
	if err := writeSupportBundle(context.Background(), &buf, opts); err != nil {
		t.Fatalf("writeSupportBundle with DATABASE_URL: %v", err)
	}
	server, ok := readBundle(t, buf.Bytes())["server.txt"]
	if !ok {
		t.Fatal("server.txt missing even though DATABASE_URL is set")
	}

	if !strings.Contains(server, "недоступна") {
		t.Fatalf("an unreachable database was not reported as such:\n%s", server)
	}
}

func TestSupportBundle_DryRunCollectsNothing(t *testing.T) {
	var out bytes.Buffer
	opts := bundleFixture(t, "http://127.0.0.1:1")
	printSupportBundlePlan(&out, opts)
	plan := out.String()

	for _, want := range []string{"versions.txt", "doctor.json", "daemon.log.txt", "config.txt", "os.txt", "network.txt"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("--dry-run does not mention %s:\n%s", want, plan)
		}
	}

	if !strings.Contains(plan, "server.txt") || !strings.Contains(plan, "DATABASE_URL") {
		t.Fatalf("--dry-run does not explain why the server section is skipped:\n%s", plan)
	}
}

func TestSupportBundle_MissingDaemonLogsAreReportedNotFatal(t *testing.T) {
	srv := fakeGoosarServer(t)
	opts := bundleFixture(t, srv.URL)
	opts.DaemonDir = filepath.Join(t.TempDir(), "never-started")

	var buf bytes.Buffer
	if err := writeSupportBundle(context.Background(), &buf, opts); err != nil {
		t.Fatalf("writeSupportBundle: %v", err)
	}
	files := readBundle(t, buf.Bytes())
	if !strings.Contains(files["daemon.log.txt"], "нет файла") {
		t.Fatalf("a missing daemon log is not explained:\n%s", files["daemon.log.txt"])
	}
}

func TestSupportBundle_UnreachableServerIsRecorded(t *testing.T) {
	opts := bundleFixture(t, "http://127.0.0.1:1")
	var buf bytes.Buffer
	if err := writeSupportBundle(context.Background(), &buf, opts); err != nil {
		t.Fatalf("writeSupportBundle: %v", err)
	}
	files := readBundle(t, buf.Bytes())
	if !strings.Contains(files["network.txt"], "недоступен") {
		t.Fatalf("an unreachable server is not reported:\n%s", files["network.txt"])
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func valuesOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestSupportBundleCmd_DryRunWritesNoFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-based fixtures are POSIX")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "bundle.tar.gz")

	var out bytes.Buffer
	cmd := newSupportBundleCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--dry-run", "--output", target})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--dry-run failed: %v (%s)", err, out.String())
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("--dry-run created an archive")
	}
	if !strings.Contains(out.String(), "manifest.txt") {
		t.Fatalf("--dry-run printed no plan:\n%s", out.String())
	}
}

func TestSupportBundle_NoSecretSurvivesInAnyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-based config fixtures are POSIX")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".goosar"), 0o700); err != nil {
		t.Fatalf("mkdir .goosar: %v", err)
	}
	cliConfig := `{"server_url":"https://goosar.example",` +
		`"token":"gsl_0123456789abcdef0123456789abcdef01234567",` +
		`"workspace_id":"018f3a2b-4c5d-6e7f-8091-a2b3c4d5e6f7"}`
	if err := os.WriteFile(filepath.Join(home, ".goosar", "config.json"), []byte(cliConfig), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	t.Setenv("GOOSAR_MCP_SECRET_KEY", "mcp-secret-value-xyz")

	srv := fakeGoosarServer(t)
	opts := bundleFixture(t, srv.URL)
	daemonDir := t.TempDir()
	daemonLog := strings.Join([]string{
		`{"level":"error","msg":"push failed","token":"mdt_89abcdef89abcdef89abcdef89abcdef89abcdef"}`,
		`{"level":"info","msg":"1c sync","password":"Zaq12wsx1C"}`,
		`Authorization: Basic c3RhdGlvbjpQYTU1dzByZFNlY3JldA==`,
		`agent token mat_11112222333344445555666677778888aaaabbbb used`,
		`connecting postgres://goosar:Pa55w0rd@db:5432/goosar`,
		`srv1c Usr="Администратор";Pwd="Qwerty123";`,
		`пароль 1С: Zaq12wsx!`,
		`req_id=c0ffee-000012 issue=018f3a2b-4c5d-6e7f-8091-a2b3c4d5e6f7 migration=000296 port=8081 pid=89123`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(daemonDir, "daemon.log"), []byte(daemonLog), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(daemonDir, "daemon.err.log"), []byte("panic: boom\n"), 0o600); err != nil {
		t.Fatalf("write daemon.err.log: %v", err)
	}
	opts.DaemonDir = daemonDir

	var buf bytes.Buffer
	if err := writeSupportBundle(context.Background(), &buf, opts); err != nil {
		t.Fatalf("writeSupportBundle: %v", err)
	}
	whole := strings.Join(valuesOf(readBundle(t, buf.Bytes())), "\n")

	for _, secret := range []string{
		"gsl_0123456789abcdef0123456789abcdef01234567",
		"mdt_89abcdef89abcdef89abcdef89abcdef89abcdef",
		"mat_11112222333344445555666677778888aaaabbbb",
		"Zaq12wsx1C",
		"Zaq12wsx!",
		"Qwerty123",
		"c3RhdGlvbjpQYTU1dzByZFNlY3JldA",
		"Pa55w0rd",
		"mcp-secret-value-xyz",
	} {
		if strings.Contains(whole, secret) {
			t.Errorf("%q reached the bundle unredacted", secret)
		}
	}
	for _, keep := range []string{
		"c0ffee-000012",
		"018f3a2b-4c5d-6e7f-8091-a2b3c4d5e6f7",
		"000296",
		"8081",
		"89123",
	} {
		if !strings.Contains(whole, keep) {
			t.Errorf("%q was redacted — the bundle lost an identifier support needs", keep)
		}
	}
}

func TestSupportBundleRedactsEnvValuesOutsideTheAllowlist(t *testing.T) {
	t.Setenv("GOOSAR_DEV_VERIFICATION_CODE", "888888")
	t.Setenv("GOOSAR_ADMIN_PIN", "hunter2xyz")
	t.Setenv("APP_ENV", "development")

	got := collectConfig(context.Background(), supportBundleOptions{})
	for _, secret := range []string{"888888", "hunter2xyz"} {
		if strings.Contains(got, secret) {
			t.Fatalf("support bundle carries %q in the clear", secret)
		}
	}
	if !strings.Contains(got, "GOOSAR_DEV_VERIFICATION_CODE=[REDACTED]") {
		t.Fatal("a redacted variable must still be reported by name")
	}
	if !strings.Contains(got, "APP_ENV=development") {
		t.Fatal("an allowlisted diagnostic variable lost its value")
	}
}

func TestSupportBundleReportsDeploymentProfileValue(t *testing.T) {
	t.Setenv("GOOSAR_DEPLOYMENT_PROFILE", "demo")

	got := collectConfig(context.Background(), supportBundleOptions{})
	if !strings.Contains(got, "GOOSAR_DEPLOYMENT_PROFILE=demo") {
		t.Fatalf("support bundle must report the deployment profile value, got:\n%s", got)
	}
}
