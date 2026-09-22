package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/realtime"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type fakeScopeQuerier struct {
	tasks    map[[16]byte]db.AgentTaskQueue
	issues   map[[16]byte]db.Issue
	sessions map[[16]byte]db.ChatSession
}

func (f *fakeScopeQuerier) GetAgentTask(_ context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	if t, ok := f.tasks[id.Bytes]; ok {
		return t, nil
	}
	return db.AgentTaskQueue{}, errors.New("not found")
}

func (f *fakeScopeQuerier) GetIssue(_ context.Context, id pgtype.UUID) (db.Issue, error) {
	if i, ok := f.issues[id.Bytes]; ok {
		return i, nil
	}
	return db.Issue{}, errors.New("not found")
}

func (f *fakeScopeQuerier) GetChatSession(_ context.Context, id pgtype.UUID) (db.ChatSession, error) {
	if s, ok := f.sessions[id.Bytes]; ok {
		return s, nil
	}
	return db.ChatSession{}, errors.New("not found")
}

func mustUUID(t *testing.T) (string, pgtype.UUID) {
	t.Helper()
	u, err := uuid.NewRandom()
	if err != nil {
		t.Fatal(err)
	}
	return u.String(), pgtype.UUID{Bytes: u, Valid: true}
}

func TestScopeAuthorizer_ChatRequiresCreator(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	creatorStr, creatorUUID := mustUUID(t)
	otherStr, _ := mustUUID(t)
	sessStr, sessUUID := mustUUID(t)
	otherWsStr, _ := mustUUID(t)
	otherWsStrOnly, otherWsUUID := mustUUID(t)
	_ = otherWsStrOnly

	q := &fakeScopeQuerier{
		sessions: map[[16]byte]db.ChatSession{
			sessUUID.Bytes: {
				ID:          sessUUID,
				WorkspaceID: wsUUID,
				CreatorID:   creatorUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	ok, err := a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeChat, sessStr)
	if err != nil || !ok {
		t.Fatalf("creator should be allowed: ok=%v err=%v", ok, err)
	}

	ok, err = a.AuthorizeScope(ctx, otherStr, wsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("peer must be denied: ok=%v err=%v", ok, err)
	}

	ok, err = a.AuthorizeScope(ctx, creatorStr, otherWsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("cross-workspace must be denied: ok=%v err=%v", ok, err)
	}
	_ = otherWsUUID

	ok, err = a.AuthorizeScope(ctx, "", wsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("empty userID must be denied: ok=%v err=%v", ok, err)
	}

	_, missingStr := mustUUID(t)
	_ = missingStr
	missingUUID, _ := uuid.NewRandom()
	ok, err = a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeChat, missingUUID.String())
	if err != nil || ok {
		t.Fatalf("unknown session must be denied: ok=%v err=%v", ok, err)
	}
}

func TestScopeAuthorizer_ChatTaskRequiresCreator(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	creatorStr, creatorUUID := mustUUID(t)
	otherStr, _ := mustUUID(t)
	sessStr, sessUUID := mustUUID(t)
	taskStr, taskUUID := mustUUID(t)
	_ = sessStr

	q := &fakeScopeQuerier{
		tasks: map[[16]byte]db.AgentTaskQueue{
			taskUUID.Bytes: {
				ID:            taskUUID,
				ChatSessionID: sessUUID,
			},
		},
		sessions: map[[16]byte]db.ChatSession{
			sessUUID.Bytes: {
				ID:          sessUUID,
				WorkspaceID: wsUUID,
				CreatorID:   creatorUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	ok, err := a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeTask, taskStr)
	if err != nil || !ok {
		t.Fatalf("creator should be allowed for chat task: ok=%v err=%v", ok, err)
	}

	ok, err = a.AuthorizeScope(ctx, otherStr, wsStr, realtime.ScopeTask, taskStr)
	if err != nil || ok {
		t.Fatalf("peer must be denied for chat task: ok=%v err=%v", ok, err)
	}
}

func TestScopeAuthorizer_IssueTaskWorkspaceOnly(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	memberStr, _ := mustUUID(t)
	otherWsStr, _ := mustUUID(t)
	taskStr, taskUUID := mustUUID(t)
	_, issueUUID := mustUUID(t)

	q := &fakeScopeQuerier{
		tasks: map[[16]byte]db.AgentTaskQueue{
			taskUUID.Bytes: {
				ID:      taskUUID,
				IssueID: issueUUID,
			},
		},
		issues: map[[16]byte]db.Issue{
			issueUUID.Bytes: {
				ID:          issueUUID,
				WorkspaceID: wsUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	ok, err := a.AuthorizeScope(ctx, memberStr, wsStr, realtime.ScopeTask, taskStr)
	if err != nil || !ok {
		t.Fatalf("member in workspace should be allowed: ok=%v err=%v", ok, err)
	}

	ok, err = a.AuthorizeScope(ctx, memberStr, otherWsStr, realtime.ScopeTask, taskStr)
	if err != nil || ok {
		t.Fatalf("cross-workspace must be denied: ok=%v err=%v", ok, err)
	}
}
