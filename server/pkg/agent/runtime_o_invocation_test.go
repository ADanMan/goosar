package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

func TestChooseRuntimeOInvocation_PassthroughForNonLauncher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	execName := "pi"
	lookedUp := filepath.Join(t.TempDir(), "pi")
	args := []string{
		"-p",
		"--mode", "json",
		"--session", "/tmp/pi-session.jsonl",
		"You are running as a chat assistant for a Goosar workspace.\n\nUser message:\n我需要创建一个issue\n",
	}

	gotExec, gotArgs := chooseRuntimeOInvocation(execName, lookedUp, args, logger)

	if gotExec != execName {
		t.Errorf("argv0 changed unexpectedly: got %q want %q", gotExec, execName)
	}
	if !reflect.DeepEqual(gotArgs, args) {
		t.Errorf("argv changed unexpectedly:\n got  %#v\n want %#v", gotArgs, args)
	}
}
