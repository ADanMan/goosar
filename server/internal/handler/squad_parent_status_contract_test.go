package handler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

func leaderCommentRuntimeBrief(t *testing.T, instructions string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := execenv.InjectRuntimeConfig(dir, "runtime-c", execenv.TaskContextForEnv{
		IssueID:           "issue-1",
		TriggerCommentID:  "comment-1",
		AgentInstructions: instructions,
		IsSquadLeader:     true,
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	return string(data)
}

func TestSquadAssignedLeaderCanWrapUpOnCommentTurn(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	squad := seedSquadForBriefing(t, leaderID, "Owning Squad", "")

	briefing := buildSquadLeaderBriefing(ctx, testHandler.Queries, squad, true)
	brief := leaderCommentRuntimeBrief(t, briefing)

	if !strings.Contains(briefing, "Own the parent issue status") {
		t.Fatalf("squad-assigned briefing must grant status ownership:\n%s", briefing)
	}

	if strings.Contains(brief, "explicitly asks for it\n") {
		t.Error("leader runtime brief still carries the unqualified no-status-change rule, " +
			"which contradicts the Own-the-parent-issue-status grant")
	}
	for _, want := range []string{

		`Squad Operating Protocol's "Own the parent issue status"`,
		"only appears when this issue is assigned to your squad",
		"without waiting to be asked",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("leader runtime brief missing %q\n--- brief ---\n%s", want, brief)
		}
	}

	combined := briefing + "\n" + brief
	if !strings.Contains(combined, "goosar issue status <issue-id> in_review") {
		t.Error("combined instructions never tell the owning leader how to wrap up")
	}
}

func TestGuestLeaderCannotChangeStatusOnCommentTurn(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	squad := seedSquadForBriefing(t, leaderID, "Guest Squad", "")

	briefing := buildSquadLeaderBriefing(ctx, testHandler.Queries, squad, false)
	brief := leaderCommentRuntimeBrief(t, briefing)

	for _, want := range []string{
		"## Squad Roster",
		"Leader (you):",
		"Delegate by @mention",
		"Record your evaluation",
	} {
		if !strings.Contains(briefing, want) {
			t.Fatalf("guest leader lost coordination context %q:\n%s", want, briefing)
		}
	}

	if strings.Contains(briefing, "Own the parent issue status") {
		t.Errorf("guest leader must not receive the status-ownership grant:\n%s", briefing)
	}
	combined := briefing + "\n" + brief
	if strings.Contains(combined, "goosar issue status <issue-id> in_review") {
		t.Error("combined instructions hand a guest leader an in_review command for " +
			"an issue assigned to someone else")
	}

	compact := strings.Join(strings.Fields(briefing), " ")
	for _, want := range []string{
		"Do NOT change this issue's status",
		"never run `goosar issue status` on it",
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("guest-leader briefing missing %q\n--- briefing ---\n%s", want, briefing)
		}
	}
}
