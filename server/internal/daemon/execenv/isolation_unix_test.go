//go:build !windows

package execenv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestPrepareIsolated_PermanentFIFOBlockThenImmediateRetry(t *testing.T) {
	sharedCodexHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedCodexHome)
	fifo := filepath.Join(sharedCodexHome, "config.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("create config FIFO: %v", err)
	}

	params := PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-isolated-prepare",
		TaskID:         "11111111-2222-3333-4444-555555555555",
		AgentName:      "isolation-test",
		Provider:       "runtime-e",
		Task: TaskContextForEnv{
			IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			AgentID: "ffffffff-1111-2222-3333-444444444444",
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	startedAt := time.Now()
	_, err := PrepareIsolated(ctx, preparationHelperTestCommand(), params, logger)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PrepareIsolated error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second {
		t.Fatalf("PrepareIsolated took %s, want a bounded process termination", elapsed)
	}
	if _, err := os.Stat(PredictRootDir(params.WorkspacesRoot, params.WorkspaceID, params.TaskID)); err != nil {
		t.Fatalf("first helper did not reach environment preparation before timeout: %v", err)
	}

	writer, writerErr := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if writerErr == nil {
		writer.Close()
		t.Fatal("timed-out preparation helper still has the FIFO open for reading")
	}
	if !errors.Is(writerErr, syscall.ENXIO) {
		t.Fatalf("probe FIFO reader: %v, want ENXIO", writerErr)
	}

	if err := os.Remove(fifo); err != nil {
		t.Fatalf("remove config FIFO: %v", err)
	}
	if err := os.WriteFile(fifo, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("replace config FIFO: %v", err)
	}
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer retryCancel()
	env, err := PrepareIsolated(retryCtx, preparationHelperTestCommand(), params, logger)
	if err != nil {
		t.Fatalf("immediate retry PrepareIsolated: %v", err)
	}
	if env == nil || env.RootDir != PredictRootDir(params.WorkspacesRoot, params.WorkspaceID, params.TaskID) {
		t.Fatalf("retry environment = %#v, want the predicted root", env)
	}
}
