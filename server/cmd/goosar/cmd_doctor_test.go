package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/cli"
)

func doctorEnv(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-executable fixtures are POSIX shell scripts")
	}
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
	return dir
}

func writeFakeBin(t *testing.T, dir, name, output string) {
	t.Helper()
	body := "#!/bin/sh\nprintf '%s\\n' '" + output + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func runDoctorCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if args == nil {
		args = []string{}
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestDoctor_FailsAndExplainsOnEmptyMachine(t *testing.T) {
	doctorEnv(t)

	out, err := runDoctorCmd(t)
	if err == nil {
		t.Fatal("goosar doctor exited 0 on a machine with no dependencies installed")
	}
	for _, want := range []string{"Не хватает", "Как починить", "Node"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output does not contain %q:\n%s", want, out)
		}
	}
}

func TestDoctor_JSONOutput(t *testing.T) {
	doctorEnv(t)

	out, _ := runDoctorCmd(t, "--output", "json")
	var report struct {
		OK      bool `json:"ok"`
		Results []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Fix    string `json:"fix"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("goosar doctor --output json did not emit JSON: %v\n%s", err, out)
	}
	if report.OK {
		t.Fatal("JSON report says ok on an empty machine")
	}
	if len(report.Results) == 0 {
		t.Fatal("JSON report carries no rows")
	}
}

func TestDoctor_PassesOnAProvisionedMachine(t *testing.T) {
	dir := doctorEnv(t)

	writeFakeBin(t, dir, "goosar", "goosar version "+version)
	writeFakeBin(t, dir, "codex", "codex 0.9.1")
	writeFakeBin(t, dir, "node", "v22.11.0")
	writeFakeBin(t, dir, "npm", "10.9.0")
	writeFakeBin(t, dir, "git", "git version 2.47.0")

	out, err := runDoctorCmd(t)
	if err != nil {
		t.Fatalf("goosar doctor failed on a provisioned machine: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Всё на месте") {
		t.Fatalf("doctor output missing the all-clear line:\n%s", out)
	}
}

func doctorStand(t *testing.T, manifest string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"server_version":"v0.11.0-test"}`)
	})
	mux.HandleFunc("/api/provisioning/manifest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Workspace-ID") == "" {
			http.Error(w, `{"error":"workspace required"}`, 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, manifest)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeDoctorProfile(t *testing.T, профиль, serverURL string) {
	t.Helper()
	cfg := cli.CLIConfig{ServerURL: serverURL, AppURL: serverURL, WorkspaceID: "ws-doctor", Token: "gsl_test"}
	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		t.Fatalf("save profile config: %v", err)
	}
}

func TestDoctorServer_WithoutFlagMakesNoNetworkCalls(t *testing.T) {
	doctorEnv(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	t.Cleanup(srv.Close)
	writeDoctorProfile(t, "doctor-nonet", srv.URL)

	out, _ := runDoctorCmd(t, "--profile", "doctor-nonet", "--output", "json")
	if hits != 0 {
		t.Fatalf("doctor without --server contacted the server %d time(s)", hits)
	}
	if strings.Contains(out, `"id":"server"`) {
		t.Fatalf("doctor without --server emitted network rows:\n%s", out)
	}
}

func TestDoctorServer_HealthyStand(t *testing.T) {
	doctorEnv(t)
	srv := doctorStand(t, `{"schemaVersion":1,"totalBeforePlatformFilter":3,"platformsAvailable":["*","darwin-arm64","linux-x64"],"packages":[{"name":"a"},{"name":"b"}],"revokedPackages":[]}`)
	writeDoctorProfile(t, "doctor-ok", srv.URL)

	out, _ := runDoctorCmd(t, "--profile", "doctor-ok", "--server", "--output", "json")
	var report struct {
		Results []struct {
			ID, Status, Detail, Message string
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	rows := map[string]struct{ Status, Detail, Message string }{}
	for _, r := range report.Results {
		rows[r.ID] = struct{ Status, Detail, Message string }{r.Status, r.Detail, r.Message}
	}
	for _, id := range []string{"config", "server", "certificate", "daemon", "kit-platform"} {
		if _, ok := rows[id]; !ok {
			t.Fatalf("no %q row in --server output:\n%s", id, out)
		}
	}
	if rows["server"].Status != "ok" || !strings.Contains(rows["server"].Detail, "v0.11.0-test") {
		t.Errorf("server row = %+v, want ok with the server version", rows["server"])
	}
	if rows["certificate"].Status != "skipped" {
		t.Errorf("certificate row = %+v, want skipped over plain HTTP", rows["certificate"])
	}
	if rows["daemon"].Status != "missing" || !strings.Contains(rows["daemon"].Message, "не запущен") {
		t.Errorf("daemon row = %+v, want «не запущен» — nothing listens on a throwaway profile's port", rows["daemon"])
	}
	if rows["kit-platform"].Status != "ok" || !strings.Contains(rows["kit-platform"].Detail, "2 доступно") {
		t.Errorf("kit row = %+v, want ok with «2 доступно»", rows["kit-platform"])
	}
}

func TestDoctorServer_PlatformGapIsNamed(t *testing.T) {
	doctorEnv(t)
	srv := doctorStand(t, `{"schemaVersion":1,"totalBeforePlatformFilter":4,"platformsAvailable":["win-x64"],"packages":[],"revokedPackages":[]}`)
	writeDoctorProfile(t, "doctor-gap", srv.URL)

	out, err := runDoctorCmd(t, "--profile", "doctor-gap", "--server")
	if err == nil {
		t.Fatal("doctor exited 0 although no package exists for this platform")
	}
	if !strings.Contains(out, "win-x64") || !strings.Contains(out, "ни одного для") {
		t.Fatalf("output does not name the platform gap:\n%s", out)
	}
}

func TestDoctorServer_DownServerIsOneRedRow(t *testing.T) {
	doctorEnv(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	writeDoctorProfile(t, "doctor-down", url)

	out, _ := runDoctorCmd(t, "--profile", "doctor-down", "--server", "--output", "json")
	var report struct {
		Results []struct{ ID, Status string } `json:"results"`
	}
	_ = json.Unmarshal([]byte(out), &report)
	got := map[string]string{}
	for _, r := range report.Results {
		got[r.ID] = r.Status
	}
	if got["server"] != "missing" {
		t.Errorf("server row = %q, want missing", got["server"])
	}
	for _, id := range []string{"certificate", "kit-platform"} {
		if got[id] != "skipped" {
			t.Errorf("%s row = %q, want skipped when the server is down (one cause, one red row)", id, got[id])
		}
	}
}
