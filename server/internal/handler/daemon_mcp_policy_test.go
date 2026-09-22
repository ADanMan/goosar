package handler

import (
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
	"github.com/adanman/goosar/server/internal/perimeterpolicy"
)

func TestDaemonMcpPolicyForDeliversLists(t *testing.T) {
	p := perimeterpolicy.ParseMCPPolicy(deliveryprofile.Cloud, "jira.corp.example", "mcp-atlassian")
	wire := daemonMcpPolicyFor(p)
	if wire == nil {
		t.Fatal("a restricted policy must be delivered to the daemon")
	}
	if !wire.HostsRestricted || !wire.CommandsRestricted {
		t.Fatalf("restricted flags = %#v", wire)
	}
	if len(wire.AllowedHosts) != 1 || wire.AllowedHosts[0] != "jira.corp.example" {
		t.Fatalf("hosts = %#v", wire.AllowedHosts)
	}
	if len(wire.AllowedCommands) != 1 || wire.AllowedCommands[0] != "mcp-atlassian" {
		t.Fatalf("commands = %#v", wire.AllowedCommands)
	}
}

func TestDaemonMcpPolicyForUnrestrictedSendsNothing(t *testing.T) {
	if wire := daemonMcpPolicyFor(perimeterpolicy.ParseMCPPolicy(deliveryprofile.Cloud, "", "")); wire != nil {
		t.Fatalf("unrestricted policy delivered %#v", wire)
	}
}
