package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

func TestGenerateTitleTruncates(t *testing.T) {
	short := generateTitle("hello world")
	if short != "hello world" {
		t.Fatalf("short content should not be truncated, got %q", short)
	}
	long := generateTitle(strings.Repeat("word ", 40))
	if len([]rune(long)) > 61 { // maxLen (60) + ellipsis rune
		t.Fatalf("long content was not truncated: len=%d", len([]rune(long)))
	}
	if !strings.HasSuffix(long, "…") {
		t.Fatalf("truncated title should end with an ellipsis, got %q", long)
	}
}

func TestCreateSessionAndSendMessage(t *testing.T) {
	db := newTestStore(t)
	f := seedAgent(t, db)
	s := NewStore(db)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, CreateSessionParams{WorkspaceID: f.WorkspaceID, AgentID: f.AgentID, CreatorID: f.AccountID})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.Status != "active" || sess.CreatorID != f.AccountID {
		t.Fatalf("unexpected session: %+v", sess)
	}

	n, err := s.CountMessages(ctx, sess.ID)
	if err != nil || n != 0 {
		t.Fatalf("CountMessages before send: %d, %v", n, err)
	}

	msg, err := s.CreateMessage(ctx, CreateMessageParams{ConvoID: sess.ID, Role: "user", Content: "Hello agent"})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if msg.Role != "user" || msg.Content != "Hello agent" || msg.MessageKind != "message" {
		t.Fatalf("unexpected message: %+v", msg)
	}

	list, err := s.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(list) != 1 || list[0].Attachments == nil {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestCanInvokePrivateAgent(t *testing.T) {
	db := newTestStore(t)
	f := seedAgent(t, db)
	s := NewStore(db)
	ctx := context.Background()

	agent, err := s.GetAgent(ctx, f.WorkspaceID, f.AgentID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}

	can, err := s.CanInvoke(ctx, agent, f.AccountID, httpapi.RoleMember)
	if err != nil || !can {
		t.Fatalf("owner should be able to invoke own private agent: can=%v err=%v", can, err)
	}

	can2, err := s.CanInvoke(ctx, agent, "00000000-0000-0000-0000-000000000000", httpapi.RoleMember)
	if err != nil || can2 {
		t.Fatalf("a stranger member should not invoke a private agent: can=%v err=%v", can2, err)
	}

	can3, err := s.CanInvoke(ctx, agent, "00000000-0000-0000-0000-000000000000", httpapi.RoleAdmin)
	if err != nil || !can3 {
		t.Fatalf("admin should bypass private ownership check: can=%v err=%v", can3, err)
	}
}

func TestPinAgentLimitAndUnpin(t *testing.T) {
	db := newTestStore(t)
	f := seedAgent(t, db)
	s := NewStore(db)
	ctx := context.Background()

	if _, err := s.PinAgent(ctx, f.AccountID, f.AgentID); err != nil {
		t.Fatalf("PinAgent: %v", err)
	}
	n, err := s.CountPinnedAgents(ctx, f.AccountID)
	if err != nil || n != 1 {
		t.Fatalf("CountPinnedAgents = %d, %v", n, err)
	}
	if err := s.UnpinAgent(ctx, f.AccountID, f.AgentID); err != nil {
		t.Fatalf("UnpinAgent: %v", err)
	}
	n2, err := s.CountPinnedAgents(ctx, f.AccountID)
	if err != nil || n2 != 0 {
		t.Fatalf("CountPinnedAgents after unpin = %d, %v", n2, err)
	}
}

func TestClaimAttachmentsIgnoresAlreadyClaimed(t *testing.T) {
	db := newTestStore(t)
	f := seedAgent(t, db)
	s := NewStore(db)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, CreateSessionParams{WorkspaceID: f.WorkspaceID, AgentID: f.AgentID, CreatorID: f.AccountID})
	if err != nil {
		t.Fatal(err)
	}
	var assetID string
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO assets (workspace_id, convo_id, as_uploader_type, as_uploader_id, as_filename, as_storage_uri,
			as_download_path, as_markdown_ref, as_content_type, as_size_bytes)
		VALUES ($1, $2, 'member', $3, 'a.png', 'local://a', '', '', 'image/png', 10) RETURNING id`,
		f.WorkspaceID, sess.ID, f.AccountID).Scan(&assetID)
	if err != nil {
		t.Fatalf("insert asset: %v", err)
	}
	msg, err := s.CreateMessage(ctx, CreateMessageParams{ConvoID: sess.ID, Role: "user", Content: "see attached"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimAttachments(ctx, f.WorkspaceID, sess.ID, msg.ID, []string{assetID, "00000000-0000-0000-0000-000000000000"})
	if err != nil {
		t.Fatalf("ClaimAttachments: %v", err)
	}
	if len(claimed) != 1 || claimed[0] != assetID {
		t.Fatalf("unexpected claimed ids: %+v", claimed)
	}

	// Второй вызов на то же вложение не должен вернуть его снова (уже занято).
	msg2, err := s.CreateMessage(ctx, CreateMessageParams{ConvoID: sess.ID, Role: "user", Content: "again"})
	if err != nil {
		t.Fatal(err)
	}
	claimedAgain, err := s.ClaimAttachments(ctx, f.WorkspaceID, sess.ID, msg2.ID, []string{assetID})
	if err != nil {
		t.Fatal(err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("expected already-claimed attachment to be ignored, got %+v", claimedAgain)
	}
}
