package daemon

import (
	"strings"
	"testing"
)

func TestBuildPrompt_HandoffNote_AssignmentBranch(t *testing.T) {
	note := "Only touch the login flow; do not change payments."
	out := BuildPrompt(Task{IssueID: "issue-123", HandoffNote: note}, "claude")

	if !strings.Contains(out, note) {
		t.Fatalf("handoff note missing from prompt:\n%s", out)
	}
	if !strings.Contains(out, "handoff note") {
		t.Fatalf("expected handoff framing in prompt:\n%s", out)
	}
	if strings.Contains(out, "quick-create assistant") {
		t.Fatalf("handoff task must not use the quick-create prompt branch:\n%s", out)
	}

	if !strings.Contains(out, "goosar issue get issue-123") {
		t.Fatalf("expected assignment prompt body:\n%s", out)
	}
}

func TestBuildPrompt_NoHandoffNote_Unchanged(t *testing.T) {
	out := BuildPrompt(Task{IssueID: "issue-123"}, "claude")
	if strings.Contains(out, "handoff note") {
		t.Fatalf("unexpected handoff framing when no note set:\n%s", out)
	}
}
