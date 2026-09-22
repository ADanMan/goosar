package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

func TestPersistSelfHostConfigIfReachable(t *testing.T) {
	t.Run("unreachable server preserves existing config and token", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		existing := cli.CLIConfig{
			ServerURL:   "https://api.old.example",
			AppURL:      "https://old.example",
			WorkspaceID: "ws-1",
			Token:       "gsl_existing_token",
		}
		if err := cli.SaveCLIConfig(existing); err != nil {
			t.Fatalf("seed config: %v", err)
		}

		proceed, _, err := persistSelfHostConfigIfReachable(
			"https://api.new.example", "https://new.example", "", "",
			func(string) (bool, string) { return false, "refused" },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if proceed {
			t.Fatalf("proceed: want false for unreachable server")
		}

		got, err := cli.LoadCLIConfig()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if got.Token != "gsl_existing_token" {
			t.Fatalf("token: want preserved, got %q", got.Token)
		}
		if got.ServerURL != "https://api.old.example" {
			t.Fatalf("server_url: want unchanged, got %q", got.ServerURL)
		}
	})

	t.Run("reachable server writes new self-host config", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		proceed, _, err := persistSelfHostConfigIfReachable(
			"https://api.new.example", "https://new.example", "", "",
			func(string) (bool, string) { return true, "" },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !proceed {
			t.Fatalf("proceed: want true for reachable server")
		}

		got, err := cli.LoadCLIConfig()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if got.ServerURL != "https://api.new.example" || got.AppURL != "https://new.example" {
			t.Fatalf("config not written: %+v", got)
		}
	})
}

func TestPersistSelfHostConfigWritesCAFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, _, err := persistSelfHostConfigIfReachable(
		"https://stand.local", "https://stand.local", "/etc/stand-root-ca.crt", "",
		func(string) (bool, string) { return true, "" },
	); err != nil {
		t.Fatal(err)
	}
	got, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.CAFile != "/etc/stand-root-ca.crt" {
		t.Fatalf("ca_file: want persisted, got %q", got.CAFile)
	}
}

func TestResolveSelfHostServerURL(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{}
		c.Flags().String("server-url", "", "")
		c.Flags().Int("port", 8080, "")
		return c
	}

	t.Run("env var honored when flag absent", func(t *testing.T) {
		t.Setenv("GOOSAR_SERVER_URL", "https://api.internal.co")
		serverURL, userProvided := resolveSelfHostServerURL(newCmd(), cli.CLIConfig{})
		if serverURL != "https://api.internal.co" {
			t.Fatalf("server_url: want env value, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for env-sourced URL")
		}
	})

	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv("GOOSAR_SERVER_URL", "https://env.example")
		cmd := newCmd()
		if err := cmd.Flags().Set("server-url", "https://flag.example"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		serverURL, userProvided := resolveSelfHostServerURL(cmd, cli.CLIConfig{})
		if serverURL != "https://flag.example" {
			t.Fatalf("server_url: want flag value, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for flag-sourced URL")
		}
	})

	t.Run("falls back to localhost with --port when neither set", func(t *testing.T) {
		t.Setenv("GOOSAR_SERVER_URL", "")
		cmd := newCmd()
		if err := cmd.Flags().Set("port", "9090"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		serverURL, userProvided := resolveSelfHostServerURL(cmd, cli.CLIConfig{})
		if serverURL != "http://localhost:9090" {
			t.Fatalf("server_url: want localhost default, got %q", serverURL)
		}
		if userProvided {
			t.Fatalf("userProvided: want false for localhost fallback")
		}
	})

	t.Run("falls back to existing config server_url when no flag, env, or --port", func(t *testing.T) {
		t.Setenv("GOOSAR_SERVER_URL", "")
		existing := cli.CLIConfig{ServerURL: "https://api.example.com"}
		serverURL, userProvided := resolveSelfHostServerURL(newCmd(), existing)
		if serverURL != "https://api.example.com" {
			t.Fatalf("server_url: want existing config value, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for config-sourced URL")
		}
	})

	t.Run("explicit --port overrides existing config server_url", func(t *testing.T) {
		t.Setenv("GOOSAR_SERVER_URL", "")
		existing := cli.CLIConfig{ServerURL: "https://api.internal.co"}
		cmd := newCmd()
		if err := cmd.Flags().Set("port", "9090"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		serverURL, userProvided := resolveSelfHostServerURL(cmd, existing)
		if serverURL != "http://localhost:9090" {
			t.Fatalf("server_url: want localhost from --port, got %q", serverURL)
		}
		if userProvided {
			t.Fatalf("userProvided: want false for localhost fallback")
		}
	})

	t.Run("flag wins over existing config", func(t *testing.T) {
		t.Setenv("GOOSAR_SERVER_URL", "")
		existing := cli.CLIConfig{ServerURL: "https://config.example"}
		cmd := newCmd()
		if err := cmd.Flags().Set("server-url", "https://flag.example"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		serverURL, _ := resolveSelfHostServerURL(cmd, existing)
		if serverURL != "https://flag.example" {
			t.Fatalf("server_url: want flag value, got %q", serverURL)
		}
	})

	t.Run("normalizes ws:// daemon form from existing config", func(t *testing.T) {
		t.Setenv("GOOSAR_SERVER_URL", "")
		existing := cli.CLIConfig{ServerURL: "wss://api.internal.co/ws"}
		serverURL, userProvided := resolveSelfHostServerURL(newCmd(), existing)
		if serverURL != "https://api.internal.co" {
			t.Fatalf("server_url: want normalized https base, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for config-sourced URL")
		}
	})

	t.Run("normalizes the documented ws:// daemon form", func(t *testing.T) {
		t.Setenv("GOOSAR_SERVER_URL", "wss://api.internal.co/ws")
		serverURL, userProvided := resolveSelfHostServerURL(newCmd(), cli.CLIConfig{})
		if serverURL != "https://api.internal.co" {
			t.Fatalf("server_url: want normalized https base, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for env-sourced URL")
		}
	})
}

func TestSelfHostAppURLHonorsEnv(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("app-url", "", "")

	t.Run("env honored when flag absent", func(t *testing.T) {
		t.Setenv("GOOSAR_APP_URL", "https://app.internal.co")
		if got := cli.FlagOrEnv(cmd, "app-url", "GOOSAR_APP_URL", ""); got != "https://app.internal.co" {
			t.Fatalf("app_url: want env value, got %q", got)
		}
	})

	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv("GOOSAR_APP_URL", "https://env.example")
		if err := cmd.Flags().Set("app-url", "https://flag.example"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if got := cli.FlagOrEnv(cmd, "app-url", "GOOSAR_APP_URL", ""); got != "https://flag.example" {
			t.Fatalf("app_url: want flag value, got %q", got)
		}
	})
}

func TestResolveSelfHostAppURL(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{}
		c.Flags().String("app-url", "", "")
		c.Flags().Int("frontend-port", 3000, "")
		return c
	}

	t.Run("flag wins over env and config", func(t *testing.T) {
		t.Setenv("GOOSAR_APP_URL", "https://env.example")
		existing := cli.CLIConfig{AppURL: "https://config.example"}
		cmd := newCmd()
		if err := cmd.Flags().Set("app-url", "https://flag.example"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if got := resolveSelfHostAppURL(cmd, existing); got != "https://flag.example" {
			t.Fatalf("app_url: want flag value, got %q", got)
		}
	})

	t.Run("env wins over config when flag absent", func(t *testing.T) {
		t.Setenv("GOOSAR_APP_URL", "https://env.example")
		existing := cli.CLIConfig{AppURL: "https://config.example"}
		if got := resolveSelfHostAppURL(newCmd(), existing); got != "https://env.example" {
			t.Fatalf("app_url: want env value, got %q", got)
		}
	})

	t.Run("falls back to existing config when no flag, env, or --frontend-port", func(t *testing.T) {
		t.Setenv("GOOSAR_APP_URL", "")
		existing := cli.CLIConfig{AppURL: "https://app.example.com"}
		if got := resolveSelfHostAppURL(newCmd(), existing); got != "https://app.example.com" {
			t.Fatalf("app_url: want existing config value, got %q", got)
		}
	})

	t.Run("explicit --frontend-port skips config fallback", func(t *testing.T) {
		t.Setenv("GOOSAR_APP_URL", "")
		existing := cli.CLIConfig{AppURL: "https://config.example"}
		cmd := newCmd()
		if err := cmd.Flags().Set("frontend-port", "4000"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if got := resolveSelfHostAppURL(cmd, existing); got != "" {
			t.Fatalf("app_url: want empty so caller infers localhost, got %q", got)
		}
	})

	t.Run("empty when nothing set", func(t *testing.T) {
		t.Setenv("GOOSAR_APP_URL", "")
		if got := resolveSelfHostAppURL(newCmd(), cli.CLIConfig{}); got != "" {
			t.Fatalf("app_url: want empty, got %q", got)
		}
	})
}

func TestSetupCallbackHostFlagWiring(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "setup", cmd: setupCmd},
		{name: "setup cloud", cmd: setupCloudCmd},
		{name: "setup self-host", cmd: setupSelfHostCmd},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.cmd.Flag(callbackHostFlag) == nil {
				t.Fatalf("%s is missing --%s", tc.name, callbackHostFlag)
			}
		})
	}
}

func TestSetupHelpShowsCallbackHostFlag(t *testing.T) {
	var out bytes.Buffer
	setupCmd.SetOut(&out)
	setupCmd.SetErr(&out)
	defer setupCmd.SetOut(nil)
	defer setupCmd.SetErr(nil)
	if err := setupCmd.Help(); err != nil {
		t.Fatalf("setup help: %v", err)
	}
	if !strings.Contains(out.String(), "--callback-host") {
		t.Fatalf("setup help should show --%s, got:\n%s", callbackHostFlag, out.String())
	}
}

func TestFormatURLChange(t *testing.T) {
	cases := []struct {
		name   string
		oldVal string
		newVal string
		want   string
	}{
		{"changed", "http://localhost:8080", "https://api.internal.co", "http://localhost:8080  ->  https://api.internal.co"},
		{"unchanged", "https://api.internal.co", "https://api.internal.co", "https://api.internal.co"},
		{"empty new keeps old", "http://localhost:8080", "", "http://localhost:8080"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := formatURLChange(tc.oldVal, tc.newVal); got != tc.want {
				t.Errorf("formatURLChange(%q, %q) = %q, want %q", tc.oldVal, tc.newVal, got, tc.want)
			}
		})
	}
}

func TestServerHostIsLocal(t *testing.T) {
	cases := []struct {
		name   string
		server string
		want   bool
	}{
		{"localhost", "http://localhost:8080", true},
		{"127.0.0.1", "http://127.0.0.1:8080", true},
		{"IPv6 loopback", "http://[::1]:8080", true},
		{"LAN IP", "http://192.168.0.28:8080", false},
		{"public FQDN", "https://api.internal.co", false},
		{"unparseable", "://bad", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := serverHostIsLocal(tc.server); got != tc.want {
				t.Errorf("serverHostIsLocal(%q) = %v, want %v", tc.server, got, tc.want)
			}
		})
	}
}
