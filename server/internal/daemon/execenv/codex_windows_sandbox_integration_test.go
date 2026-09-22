package execenv

import (
	"path/filepath"
	"testing"

	"github.com/adanman/goosar/server/pkg/agent"
)

func TestWindowsSandboxHonorsShellQuotedCustomArg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  []string
	}{
		{"both tokens single-quoted", []string{"'-c'", "'windows.sandbox=unelevated'"}},
		{"value double-quoted", []string{"-c", `"windows.sandbox=elevated"`}},
		{"inline flag single-quoted", []string{"'-c=windows.sandbox=unelevated'"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := windowsSandboxFromCustomArgs(tc.raw); got == windowsSandboxNative {
				t.Fatalf("raw shell-quoted args unexpectedly recognized without normalization: %v", tc.raw)
			}

			norm := agent.NormalizeRuntimeELaunchArgs(nil, tc.raw, nil, testLogger())
			missing := filepath.Join(t.TempDir(), "config.toml")
			state := resolveWindowsSandboxState(missing, nil, sharedConfigAbsent, norm, testLogger())
			if state != windowsSandboxNative {
				t.Fatalf("normalized %v -> state %v, want native", norm, state)
			}
			if p := codexSandboxPolicyForWindows(state); p.Mode != "workspace-write" {
				t.Fatalf("policy mode = %q, want workspace-write (isolation preserved)", p.Mode)
			}
		})
	}
}
