package handler

import (
	"testing"

	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

func TestRelativeWorkDir(t *testing.T) {
	const (
		wsID   = "a05b0e10-ee7a-4603-a72d-a548b2390cb2"
		taskID = "5c57b65b-ee7a-4603-a72d-a548b2390cb2"
	)

	tests := []struct {
		name     string
		workDir  string
		wsID     string
		taskID   string
		expected string
	}{
		{
			name:     "empty work_dir returns empty",
			workDir:  "",
			wsID:     wsID,
			taskID:   taskID,
			expected: "",
		},
		{
			name:     "standard envRoot path strips workspaces root",
			workDir:  "/Users/alice/goosar_workspaces/" + wsID + "/a548b2390cb2/workdir",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/a548b2390cb2/workdir",
		},
		{
			name:     "standard envRoot path without trailing workdir",
			workDir:  "/Users/alice/goosar_workspaces/" + wsID + "/a548b2390cb2",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/a548b2390cb2",
		},
		{
			name:     "local_directory path under /Users home is stripped",
			workDir:  "/Users/carol/repos/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "repos/foo",
		},
		{
			name:     "local_directory deep path under home keeps full remainder",
			workDir:  "/Users/carol/code/work/projects/goosar/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "code/work/projects/goosar/foo",
		},
		{
			name:     "shallow /Users home path strips username segment",
			workDir:  "/Users/alice/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "foo",
		},
		{
			name:     "shallow Linux /home path strips username segment",
			workDir:  "/home/alice/project",
			wsID:     wsID,
			taskID:   taskID,
			expected: "project",
		},
		{
			name:     "shallow Windows /Users path strips username segment",
			workDir:  `C:\Users\alice\foo`,
			wsID:     wsID,
			taskID:   taskID,
			expected: "foo",
		},
		{
			name:     "exact home directory returns empty (would only render username)",
			workDir:  "/Users/alice",
			wsID:     wsID,
			taskID:   taskID,
			expected: "",
		},
		{
			name:     "exact home directory with trailing slash returns empty",
			workDir:  "/Users/alice/",
			wsID:     wsID,
			taskID:   taskID,
			expected: "",
		},
		{
			name:     "Windows local_directory path under home strips username",
			workDir:  `C:\Users\alice\repos\foo`,
			wsID:     wsID,
			taskID:   taskID,
			expected: "repos/foo",
		},
		{
			name:     "non-home local path falls back to basename only",
			workDir:  "/opt/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "foo",
		},
		{
			name:     "non-home deep local path falls back to basename only",
			workDir:  "/srv/git/repo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "repo",
		},
		{
			name:     "single-segment local path returns the segment",
			workDir:  "/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "foo",
		},
		{
			name:     "Windows backslash separators are normalized",
			workDir:  `C:\Users\alice\goosar_workspaces\` + wsID + `\a548b2390cb2\workdir`,
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/a548b2390cb2/workdir",
		},
		{
			name:     "missing workspace_id under home strips home prefix instead of envRoot",
			workDir:  "/Users/alice/goosar_workspaces/" + wsID + "/a548b2390cb2/workdir",
			wsID:     "",
			taskID:   taskID,
			expected: "goosar_workspaces/" + wsID + "/a548b2390cb2/workdir",
		},
		{
			name:     "missing task_id under home strips home prefix instead of envRoot",
			workDir:  "/Users/alice/goosar_workspaces/" + wsID + "/a548b2390cb2/workdir",
			wsID:     wsID,
			taskID:   "",
			expected: "goosar_workspaces/" + wsID + "/a548b2390cb2/workdir",
		},
		{
			name:     "trailing slash on envRoot path is preserved in returned suffix",
			workDir:  "/Users/alice/goosar_workspaces/" + wsID + "/a548b2390cb2/workdir/",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/a548b2390cb2/workdir/",
		},
		{
			name:     "wsID prefix appearing elsewhere falls back to basename when not under home",
			workDir:  "/var/" + wsID + "/something/else",
			wsID:     wsID,
			taskID:   taskID,
			expected: "else",
		},
		{
			name:     "case-insensitive /users matches the same as /Users",
			workDir:  "/users/alice/repos/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "repos/foo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := relativeWorkDir(tc.workDir, tc.wsID, tc.taskID)
			if got != tc.expected {
				t.Fatalf("relativeWorkDir(%q, %q, %q) = %q, want %q",
					tc.workDir, tc.wsID, tc.taskID, got, tc.expected)
			}
		})
	}
}

func TestShortTaskIDMatchesDaemon(t *testing.T) {
	const (
		workspacesRoot = "/tmp/workspaces"
		workspaceID    = "a05b0e10-ee7a-4603-a72d-a548b2390cb2"
		taskID         = "5c57b65b-ee7a-4603-a72d-a548b2390cb2"
	)
	daemonRoot := execenv.PredictRootDir(workspacesRoot, workspaceID, taskID)
	expected := workspacesRoot + "/" + workspaceID + "/" + taskDirSegment(taskID)
	if daemonRoot != expected {
		t.Fatalf("daemon PredictRootDir = %q, handler-side reconstruction = %q — taskDirSegment is out of sync with execenv.taskKey", daemonRoot, expected)
	}
}
