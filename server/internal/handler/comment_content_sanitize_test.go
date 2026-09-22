package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
)

func TestCreateComment_StripsNullBytesInsteadOf500(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	issueID := createTestIssue(t, "null-byte comment fixture (GH #5388)", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "diagnosis body\x00 with a stray NUL byte",
	})
	r = withURLParam(r, "id", issueID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment with NUL byte: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got, _ := body["content"].(string)
	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("stored content still contains a NUL byte: %q", got)
	}
	if want := "diagnosis body with a stray NUL byte"; got != want {
		t.Fatalf("stored content: expected %q (NUL stripped), got %q", want, got)
	}
}

func TestCommentTriggers_NullByteHiddenMention_PreviewMatchesEnqueue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	issueID := createCommentTriggerPreviewIssue(t, "NUL-hidden mention parity (GH #5388 review)", "", "")

	createTarget := createHandlerTestAgent(t, "Preview NUL Parity Create", nil)
	editTarget := createHandlerTestAgent(t, "Preview NUL Parity Edit", nil)

	mentionWithHiddenNull := func(agentID string) string {
		return fmt.Sprintf("please take a look [@Target](mention://agent/%s\x00)", agentID)
	}
	previewAgentIDs := func(resp CommentTriggerPreviewResponse) map[string]bool {
		ids := make(map[string]bool, len(resp.Agents))
		for _, a := range resp.Agents {
			ids[a.ID] = true
		}
		return ids
	}

	createContent := mentionWithHiddenNull(createTarget)
	if n := len(util.ParseMentions(createContent)); n != 0 {
		t.Fatalf("precondition: raw NUL content should not parse as a mention, got %d", n)
	}
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": createContent})
	if ids := previewAgentIDs(preview); len(ids) != 1 || !ids[createTarget] {
		t.Fatalf("create preview targets = %v, want exactly {%s}", ids, createTarget)
	}
	postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": createContent})
	if got := countQueuedCommentTriggerTasks(t, issueID, createTarget); got != 1 {
		t.Fatalf("create enqueued %d tasks for the mentioned agent, want 1 (parity with preview)", got)
	}

	editContent := mentionWithHiddenNull(editTarget)
	plainID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": "no mentions here yet"})
	editPreview := previewCommentTriggersForTest(t, issueID, map[string]any{
		"content":            editContent,
		"editing_comment_id": plainID,
	})
	if ids := previewAgentIDs(editPreview); len(ids) != 1 || !ids[editTarget] {
		t.Fatalf("edit preview targets = %v, want exactly {%s}", ids, editTarget)
	}
	updateCommentForTriggerPreviewTest(t, plainID, map[string]any{"content": editContent})
	if got := countQueuedCommentTriggerTasks(t, issueID, editTarget); got != 1 {
		t.Fatalf("edit enqueued %d tasks for the mentioned agent, want 1 (parity with preview)", got)
	}
}
