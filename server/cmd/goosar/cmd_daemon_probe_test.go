package main

import (
	"testing"

	"github.com/adanman/goosar/server/internal/daemon"
)

func TestDaemonRuntimeProbeFromAgents(t *testing.T) {
	probe := daemonRuntimeProbeFromAgents(map[string]daemon.AgentEntry{
		"claude": {},
		"codex":  {},
	})
	if probe.ProbeResult != "success" || probe.RuntimeCount != 2 || probe.ProviderSummary["codex"] != 1 {
		t.Fatalf("probe = %#v", probe)
	}
}

func TestLoadConfigAllowNoAgentsIsOptIn(t *testing.T) {

	var overrides daemon.Overrides
	if overrides.AllowNoAgents {
		t.Fatal("AllowNoAgents must default to false")
	}
}
