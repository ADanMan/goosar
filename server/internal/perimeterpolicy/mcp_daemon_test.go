package perimeterpolicy

import (
	"encoding/json"
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

func TestMCPPolicyListsRoundTrip(t *testing.T) {
	p := ParseMCPPolicy(deliveryprofile.Cloud, "jira.corp.example", "mcp-atlassian")
	hosts, hostsRestricted, commands, commandsRestricted := p.Lists()
	if !hostsRestricted || !commandsRestricted {
		t.Fatalf("restricted flags = %v/%v", hostsRestricted, commandsRestricted)
	}
	rebuilt := NewMCPPolicy(hosts, hostsRestricted, commands, commandsRestricted)
	if v := rebuilt.CheckEntry("ok", json.RawMessage(`{"command":"mcp-atlassian"}`)); v != nil {
		t.Fatalf("allowed entry rejected: %s", v.Reason)
	}
	if v := rebuilt.CheckEntry("bad", json.RawMessage(`{"command":"mcp-remote","args":["https://evil.example"]}`)); v == nil {
		t.Fatal("expected the rebuilt policy to reject a non-allowlisted command")
	}
}

func TestMCPPolicyListsNilIsUnrestricted(t *testing.T) {
	var p *MCPPolicy
	hosts, hostsRestricted, commands, commandsRestricted := p.Lists()
	if hostsRestricted || commandsRestricted || hosts != nil || commands != nil {
		t.Fatalf("nil policy reported as restricted: %v %v %v %v", hosts, hostsRestricted, commands, commandsRestricted)
	}
	if NewMCPPolicy(nil, false, nil, false) != nil {
		t.Fatal("an unrestricted wire payload must rebuild as a nil policy")
	}
}

func TestNewMCPPolicyEmptyRestrictedDeniesEverything(t *testing.T) {
	p := NewMCPPolicy(nil, true, nil, true)
	if v := p.CheckEntry("x", json.RawMessage(`{"command":"anything"}`)); v == nil {
		t.Fatal("empty allowlist must deny")
	}
}
