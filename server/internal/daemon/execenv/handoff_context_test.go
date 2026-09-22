package execenv

import (
	"strings"
	"testing"
)

func TestRenderIssueContext_HandoffNote(t *testing.T) {
	note := "Scope to the auth module only."
	md := renderIssueContext("runtime-c", TaskContextForEnv{IssueID: "issue-1", HandoffNote: note})

	if !strings.Contains(md, "## Handoff Note") {
		t.Fatalf("expected Handoff Note section:\n%s", md)
	}
	if !strings.Contains(md, note) {
		t.Fatalf("handoff note text missing:\n%s", md)
	}
	if !strings.Contains(md, "**Trigger:** New Assignment") {
		t.Fatalf("handoff note must render under the assignment trigger:\n%s", md)
	}
}

func TestRenderIssueContext_NoHandoffNote(t *testing.T) {
	md := renderIssueContext("runtime-c", TaskContextForEnv{IssueID: "issue-1"})
	if strings.Contains(md, "## Handoff Note") {
		t.Fatalf("unexpected Handoff Note section when no note set:\n%s", md)
	}
}
