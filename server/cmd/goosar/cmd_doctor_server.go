package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/preflight"
)

func doctorServerOptions(профиль string) preflight.ServerOptions {
	cfg, _ := cli.LoadCLIConfigForProfile(профиль)
	path, _ := cli.CLIConfigPathForProfile(профиль)
	serverURL := strings.TrimSpace(cfg.ServerURL)
	opts := preflight.ServerOptions{
		ConfigPath: path,
		Profile:    профиль,
		ServerURL:  serverURL,
		CAFile:     cli.ResolveCAFile("", cfg),
		Platform:   clientPlatformTag(),
	}
	if serverURL == "" {
		return opts
	}
	api := cli.NewAPIClient(serverURL, cfg.WorkspaceID, cfg.Token)
	opts.ProbeServer = func(ctx context.Context) preflight.ServerProbe {
		ok, reason, err := probeServerDetail(serverURL)
		probe := preflight.ServerProbe{OK: ok, Reason: reason, TLSUntrusted: isUntrustedCertificate(err)}
		if ok {
			var conf struct {
				ServerVersion string `json:"server_version"`
			}
			if api.GetJSON(ctx, "/api/config", &conf) == nil {
				probe.Version = conf.ServerVersion
			}
		}
		return probe
	}
	opts.DaemonHealth = func(ctx context.Context) preflight.DaemonProbe {
		return probeDaemonHealth(ctx, healthPortForProfile(профиль))
	}
	opts.KitManifest = func(ctx context.Context) (preflight.KitProbe, error) {
		if strings.TrimSpace(cfg.WorkspaceID) == "" {
			return preflight.KitProbe{}, errors.New("в конфигурации нет workspace_id — выполните goosar login")
		}
		var m struct {
			Total     int               `json:"totalBeforePlatformFilter"`
			Platforms []string          `json:"platformsAvailable"`
			Packages  []json.RawMessage `json:"packages"`
		}
		if err := api.GetJSON(ctx, "/api/provisioning/manifest?platform="+opts.Platform, &m); err != nil {
			return preflight.KitProbe{}, err
		}
		return preflight.KitProbe{Total: m.Total, ForPlatform: len(m.Packages), Platforms: m.Platforms}, nil
	}
	opts.InstalledPackages = countInstalledPackages
	opts.LLMHealth = func(ctx context.Context) (preflight.LLMHealthProbe, error) {
		var health struct {
			Status    string `json:"status"`
			LatencyMS int64  `json:"latency_ms"`
		}
		if err := api.GetJSON(ctx, "/api/llm/health", &health); err != nil {
			return preflight.LLMHealthProbe{}, err
		}
		return preflight.LLMHealthProbe{Status: health.Status, LatencyMS: health.LatencyMS}, nil
	}
	return opts
}

func isUntrustedCertificate(err error) bool {
	if err == nil {
		return false
	}
	var unknown x509.UnknownAuthorityError
	var verify *tls.CertificateVerificationError
	if errors.As(err, &unknown) || errors.As(err, &verify) {
		return true
	}
	return strings.Contains(err.Error(), "certificate signed by unknown authority")
}

func probeDaemonHealth(ctx context.Context, port int) preflight.DaemonProbe {
	addr := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	detail := fmt.Sprintf("порт %d", port)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return preflight.DaemonProbe{Status: preflight.DaemonInvalid, Detail: detail}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		var opErr *net.OpError
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return preflight.DaemonProbe{Status: preflight.DaemonUnreachable, Detail: detail}
		case errors.As(err, &opErr) && strings.Contains(err.Error(), "refused"):
			return preflight.DaemonProbe{Status: preflight.DaemonStopped, Detail: detail}
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return preflight.DaemonProbe{Status: preflight.DaemonUnreachable, Detail: detail}
		}
		return preflight.DaemonProbe{Status: preflight.DaemonStopped, Detail: detail + ": " + err.Error()}
	}
	defer resp.Body.Close()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Status == "" {
		return preflight.DaemonProbe{Status: preflight.DaemonInvalid, Detail: detail}
	}
	switch body.Status {
	case preflight.DaemonRunning, preflight.DaemonStarting:
		return preflight.DaemonProbe{Status: body.Status, Detail: detail}
	default:
		return preflight.DaemonProbe{Status: preflight.DaemonStopped, Detail: detail + ", status=" + body.Status}
	}
}

func clientPlatformTag() string {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		arch = runtime.GOARCH
	}
	goos := runtime.GOOS
	if goos == "windows" {
		goos = "win"
	}
	return goos + "-" + arch
}

func countInstalledPackages() int {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	n := 0
	for _, store := range []string{"skills", "mcp-servers", "runtime"} {
		entries, err := os.ReadDir(filepath.Join(home, ".hermes", store))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				n++
			}
		}
	}
	return n
}
