package handler

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisTestDB = 14

func newRedisTestClient(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse REDIS_TEST_URL: %v", err)
	}
	opts.DB = redisTestDB
	rdb := redis.NewClient(opts)
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("REDIS_TEST_URL unreachable: %v", err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	t.Cleanup(func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	})
	return rdb
}

func TestRedisLocalSkillListStore_CreateGetComplete(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	req, err := store.Create(ctx, "runtime-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.Status != RuntimeLocalSkillPending {
		t.Fatalf("initial status = %s", req.Status)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != req.ID {
		t.Fatalf("round trip lost id: got=%v", got)
	}

	skills := []RuntimeLocalSkillSummary{
		{
			Key:         "review-helper",
			Name:        "Review Helper",
			Description: "Review PRs",
			SourcePath:  "~/.claude/skills/review-helper",
			Provider:    "runtime-c",
			FileCount:   2,
		},
	}
	mcpServers := []RuntimeLocalMcpServerSummary{
		{Name: "fetch", Transport: "stdio", Source: "User config", Enabled: true},
	}
	if err := store.Complete(ctx, req.ID, skills, true, mcpServers, true); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, err = store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after complete: %v", err)
	}
	if got.Status != RuntimeLocalSkillCompleted {
		t.Fatalf("status after complete = %s", got.Status)
	}
	if len(got.Skills) != 1 || got.Skills[0].Key != "review-helper" {
		t.Fatalf("skills not persisted: %+v", got.Skills)
	}
	if !got.McpSupported || len(got.McpServers) != 1 || got.McpServers[0].Name != "fetch" {
		t.Fatalf("MCP inventory not persisted: %+v", got.McpServers)
	}
}

func TestRedisLocalSkillListStore_PopPendingAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisLocalSkillListStore(rdb)
	nodeB := NewRedisLocalSkillListStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node A create: %v", err)
	}

	popped, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B pop: %v", err)
	}
	if popped == nil {
		t.Fatal("node B did not see node A's pending request")
	}
	if popped.ID != req.ID {
		t.Fatalf("popped id = %s, want %s", popped.ID, req.ID)
	}
	if popped.Status != RuntimeLocalSkillRunning {
		t.Fatalf("popped status = %s, want running", popped.Status)
	}
	if popped.RunStartedAt == nil {
		t.Fatal("run_started_at not set after pop")
	}

	again, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B second pop: %v", err)
	}
	if again != nil {
		t.Fatalf("expected no more pending, got %+v", again)
	}
}

func TestRedisLocalSkillListStore_PopPendingConcurrent(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	req, err := store.Create(ctx, "runtime-race")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const N = 8
	var wg sync.WaitGroup
	results := make(chan *RuntimeLocalSkillListRequest, N)
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			popped, err := store.PopPending(ctx, "runtime-race")
			if err != nil {
				errs <- err
				return
			}
			results <- popped
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent pop error: %v", err)
	}

	winners := 0
	for popped := range results {
		if popped != nil {
			winners++
			if popped.ID != req.ID {
				t.Fatalf("winner popped wrong id: %s", popped.ID)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d", winners)
	}
}

func TestRedisLocalSkillListStore_PendingTimeout(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	req, err := store.Create(ctx, "runtime-timeout")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	req.CreatedAt = time.Now().Add(-runtimeLocalSkillPendingTimeout - time.Second)
	if err := store.persistListRequest(ctx, req); err != nil {
		t.Fatalf("persist rewound: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != RuntimeLocalSkillTimeout {
		t.Fatalf("status = %s, want timeout", got.Status)
	}

	popped, err := store.PopPending(ctx, "runtime-timeout")
	if err != nil {
		t.Fatalf("pop after timeout: %v", err)
	}
	if popped != nil {
		t.Fatalf("expected no pending after timeout, got %+v", popped)
	}
}

func TestRedisLocalSkillImportStore_PreservesCreatorID(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillImportStore(rdb)

	name := "Review Helper"
	desc := "Desc"
	req, err := store.Create(ctx, LocalSkillImportRequestInput{
		RuntimeID:     "runtime-1",
		CreatorID:     "user-42",
		SkillKey:      "review-helper",
		Name:          &name,
		Description:   &desc,
		Action:        LocalSkillImportActionOverwrite,
		TargetSkillID: "target-skill-99",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.CreatorID != "user-42" {
		t.Fatalf("creator id lost on create")
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.CreatorID != "user-42" {
		t.Fatalf("creator id lost round trip: %q", got.CreatorID)
	}
	if got.Name == nil || *got.Name != name {
		t.Fatalf("name lost: %v", got.Name)
	}
	if got.Description == nil || *got.Description != desc {
		t.Fatalf("description lost: %v", got.Description)
	}

	if got.Action != LocalSkillImportActionOverwrite {
		t.Fatalf("action lost round trip: %q", got.Action)
	}
	if got.TargetSkillID != "target-skill-99" {
		t.Fatalf("target_skill_id lost round trip: %q", got.TargetSkillID)
	}
}

func TestRedisLocalSkillImportStore_CompletePreservesFiles(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillImportStore(rdb)

	req, err := store.Create(ctx, LocalSkillImportRequestInput{
		RuntimeID: "runtime-1",
		CreatorID: "user-1",
		SkillKey:  "review-helper",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	completedAt := time.Now().UTC().Format(time.RFC3339)
	skill := SkillWithFilesResponse{
		SkillResponse: SkillResponse{
			ID:          "skill-1",
			WorkspaceID: testWorkspaceID,
			Name:        "Review Helper",
			Description: "Review PRs",
			Content:     "# Review Helper",
			CreatedAt:   completedAt,
			UpdatedAt:   completedAt,
		},
		Files: []SkillFileResponse{
			{
				ID:        "file-1",
				SkillID:   "skill-1",
				Path:      "rules.md",
				Content:   "Use the review checklist.",
				CreatedAt: completedAt,
				UpdatedAt: completedAt,
			},
		},
	}
	if err := store.Complete(ctx, req.ID, skill); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != RuntimeLocalSkillCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	if got.Skill == nil {
		t.Fatal("completed skill lost round trip")
	}
	if len(got.Skill.Files) != 1 {
		t.Fatalf("files lost round trip: %+v", got.Skill.Files)
	}
	if got.Skill.Files[0].Path != "rules.md" || got.Skill.Files[0].Content != "Use the review checklist." {
		t.Fatalf("file corrupted round trip: %+v", got.Skill.Files[0])
	}
}

func TestRedisLocalSkillImportStore_PreservesConflict(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillImportStore(rdb)

	req, err := store.Create(ctx, LocalSkillImportRequestInput{
		RuntimeID: "runtime-1",
		CreatorID: "user-1",
		SkillKey:  "review-helper",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	info := LocalSkillImportConflict{ExistingSkillID: "skill-7", ExistingCreatedBy: "user-2", CanOverwrite: false}
	if err := store.Conflict(ctx, req.ID, info); err != nil {
		t.Fatalf("conflict: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != RuntimeLocalSkillConflict {
		t.Fatalf("status = %s, want conflict", got.Status)
	}
	if got.Conflict == nil {
		t.Fatalf("conflict metadata lost round trip")
	}
	if got.Conflict.ExistingSkillID != "skill-7" || got.Conflict.ExistingCreatedBy != "user-2" || got.Conflict.CanOverwrite {
		t.Fatalf("conflict metadata corrupted: %+v", got.Conflict)
	}
}

func TestRedisLocalSkillImportStore_PopPendingAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisLocalSkillImportStore(rdb)
	nodeB := NewRedisLocalSkillImportStore(rdb)

	req, err := nodeA.Create(ctx, LocalSkillImportRequestInput{
		RuntimeID: "runtime-import",
		CreatorID: "user-1",
		SkillKey:  "review-helper",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	popped, err := nodeB.PopPending(ctx, "runtime-import")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if popped == nil || popped.ID != req.ID {
		t.Fatalf("cross-node pop failed: got %+v", popped)
	}
	if popped.Status != RuntimeLocalSkillRunning {
		t.Fatalf("popped status = %s", popped.Status)
	}
	if popped.SkillKey != "review-helper" {
		t.Fatalf("skill_key lost: %q", popped.SkillKey)
	}
}

func TestRedisLocalSkillListStore_PerRuntimeIsolation(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	if _, err := store.Create(ctx, "runtime-A"); err != nil {
		t.Fatalf("create A: %v", err)
	}
	reqB, err := store.Create(ctx, "runtime-B")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	popped, err := store.PopPending(ctx, "runtime-B")
	if err != nil {
		t.Fatalf("pop B: %v", err)
	}
	if popped == nil || popped.ID != reqB.ID {
		t.Fatalf("pop returned wrong request: %+v", popped)
	}

	ids, err := rdb.ZRange(ctx, localSkillListPendingKey("runtime-A"), 0, -1).Result()
	if err != nil {
		t.Fatalf("zrange A: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending for A after pop(B), got %d: %v", len(ids), ids)
	}
}

func TestRedisLocalSkillListStore_PopPendingAtomicClaim(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	req, err := store.Create(ctx, "runtime-atomic")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	popped, err := store.PopPending(ctx, "runtime-atomic")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if popped == nil || popped.ID != req.ID {
		t.Fatalf("pop returned wrong request: %+v", popped)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after pop: %v", err)
	}
	if got.Status != RuntimeLocalSkillRunning {
		t.Fatalf("record status = %s, want running", got.Status)
	}

	again, err := store.PopPending(ctx, "runtime-atomic")
	if err != nil {
		t.Fatalf("second pop: %v", err)
	}
	if again != nil {
		t.Fatalf("second pop should be empty, got %+v", again)
	}
}

func TestRedisLocalSkillImportStore_PopPendingBatch(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillImportStore(rdb)

	ids := make([]string, 5)
	for i := range ids {
		req, err := store.Create(ctx, LocalSkillImportRequestInput{
			RuntimeID: "runtime-batch",
			CreatorID: "user-1",
			SkillKey:  fmt.Sprintf("skill-%d", i),
		})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ids[i] = req.ID
	}

	batch, err := store.PopPendingBatch(ctx, "runtime-batch", 3)
	if err != nil {
		t.Fatalf("pop batch: %v", err)
	}
	if len(batch) != 3 {
		t.Fatalf("expected 3, got %d", len(batch))
	}
	for _, req := range batch {
		if req.Status != RuntimeLocalSkillRunning {
			t.Fatalf("batch item status = %s, want running", req.Status)
		}
	}

	rest, err := store.PopPendingBatch(ctx, "runtime-batch", 10)
	if err != nil {
		t.Fatalf("pop rest: %v", err)
	}
	if len(rest) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(rest))
	}

	empty, err := store.PopPendingBatch(ctx, "runtime-batch", 10)
	if err != nil {
		t.Fatalf("pop empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0, got %d", len(empty))
	}
}

var (
	_ LocalSkillListStore   = (*RedisLocalSkillListStore)(nil)
	_ LocalSkillImportStore = (*RedisLocalSkillImportStore)(nil)
	_ LocalSkillListStore   = (*InMemoryLocalSkillListStore)(nil)
	_ LocalSkillImportStore = (*InMemoryLocalSkillImportStore)(nil)
)
