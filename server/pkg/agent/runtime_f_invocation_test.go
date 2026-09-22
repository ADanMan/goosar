package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

func TestChooseRuntimeFInvocation_PassthroughForNonLauncher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	execName := "copilot"
	lookedUp := filepath.Join(t.TempDir(), "copilot")
	args := []string{
		"-p", "You are running as a local coding agent.\n\nDo something.",
		"--output-format", "json",
		"--allow-all",
		"--no-ask-user",
	}

	gotExec, gotArgs := chooseRuntimeFInvocation(execName, lookedUp, args, logger)

	if gotExec != execName {
		t.Errorf("argv0 changed unexpectedly: got %q want %q", gotExec, execName)
	}
	if !reflect.DeepEqual(gotArgs, args) {
		t.Errorf("argv changed unexpectedly:\n got  %#v\n want %#v", gotArgs, args)
	}
}
