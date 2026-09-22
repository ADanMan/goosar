package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

type TaskDiskUsage struct {
	WorkspaceID       string `json:"workspace_id"`
	WorkspaceShort    string `json:"workspace_short"`
	TaskShort         string `json:"task_short"`
	Path              string `json:"path"`
	Kind              string `json:"kind"`
	ParentStatus      string `json:"parent_status"`
	AgeSeconds        int64  `json:"age_seconds"`
	SizeBytes         int64  `json:"size_bytes"`
	ArtifactSizeBytes int64  `json:"artifact_size_bytes"`
}

type WorkspaceDiskUsage struct {
	WorkspaceID       string  `json:"workspace_id"`
	WorkspaceShort    string  `json:"workspace_short"`
	TaskCount         int     `json:"task_count"`
	SizeBytes         int64   `json:"size_bytes"`
	ArtifactSizeBytes int64   `json:"artifact_size_bytes"`
	ArtifactRatio     float64 `json:"artifact_ratio"`
	OldestAgeSeconds  int64   `json:"oldest_age_seconds"`
}

type DiskUsageReport struct {
	WorkspacesRoot          string               `json:"workspaces_root"`
	GeneratedAt             time.Time            `json:"generated_at"`
	ArtifactPatterns        []string             `json:"artifact_patterns"`
	ManagedArtifactSubpaths []string             `json:"managed_artifact_subpaths"`
	Tasks                   []TaskDiskUsage      `json:"tasks"`
	Workspaces              []WorkspaceDiskUsage `json:"workspaces"`
	TotalTaskCount          int                  `json:"total_task_count"`
	TotalWorkspaceCount     int                  `json:"total_workspace_count"`
	TotalSizeBytes          int64                `json:"total_size_bytes"`
	TotalArtifactSizeBytes  int64                `json:"total_artifact_size_bytes"`
	TotalArtifactRatio      float64              `json:"total_artifact_ratio"`
}

type DiskUsageRoot struct {
	Profile string
	Root    string
}

type RootDiskUsage struct {
	Profile string          `json:"profile"`
	Report  DiskUsageReport `json:"report"`
}

type AggregateDiskUsageReport struct {
	GeneratedAt             time.Time       `json:"generated_at"`
	ArtifactPatterns        []string        `json:"artifact_patterns"`
	ManagedArtifactSubpaths []string        `json:"managed_artifact_subpaths"`
	Roots                   []RootDiskUsage `json:"roots"`
	TotalTaskCount          int             `json:"total_task_count"`
	TotalWorkspaceCount     int             `json:"total_workspace_count"`
	TotalSizeBytes          int64           `json:"total_size_bytes"`
	TotalArtifactSizeBytes  int64           `json:"total_artifact_size_bytes"`
	TotalArtifactRatio      float64         `json:"total_artifact_ratio"`
}

func ScanDiskUsageRoots(roots []DiskUsageRoot, artifactPatterns []string) (AggregateDiskUsageReport, error) {
	agg := AggregateDiskUsageReport{GeneratedAt: time.Now().UTC()}
	matcher := newArtifactMatcher(artifactPatterns, execenv.ManagedReclaimableArtifactSubpaths())
	agg.ArtifactPatterns = sortedKeys(matcher.basenames)
	agg.ManagedArtifactSubpaths = matcher.managedSubpaths()

	for _, r := range roots {
		report, err := ScanDiskUsage(r.Root, artifactPatterns)
		if err != nil {
			return agg, err
		}
		agg.Roots = append(agg.Roots, RootDiskUsage{Profile: r.Profile, Report: report})
		agg.TotalTaskCount += report.TotalTaskCount
		agg.TotalWorkspaceCount += report.TotalWorkspaceCount
		agg.TotalSizeBytes += report.TotalSizeBytes
		agg.TotalArtifactSizeBytes += report.TotalArtifactSizeBytes
	}
	agg.TotalArtifactRatio = ratio(agg.TotalArtifactSizeBytes, agg.TotalSizeBytes)
	return agg, nil
}

const DiskUsageKindUnknown = "unknown"

func ScanDiskUsage(workspacesRoot string, artifactPatterns []string) (DiskUsageReport, error) {
	report := DiskUsageReport{
		WorkspacesRoot:   workspacesRoot,
		GeneratedAt:      time.Now().UTC(),
		ArtifactPatterns: nil,
	}
	if workspacesRoot == "" {
		return report, fmt.Errorf("disk-usage: workspaces root is required")
	}

	matcher := newArtifactMatcher(artifactPatterns, execenv.ManagedReclaimableArtifactSubpaths())
	report.ArtifactPatterns = sortedKeys(matcher.basenames)
	report.ManagedArtifactSubpaths = matcher.managedSubpaths()

	wsEntries, err := os.ReadDir(workspacesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, fmt.Errorf("disk-usage: read workspaces root: %w", err)
	}

	wsAgg := map[string]*WorkspaceDiskUsage{}

	for _, wsEntry := range wsEntries {

		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		wsID := wsEntry.Name()
		wsDir := filepath.Join(workspacesRoot, wsID)
		taskEntries, err := os.ReadDir(wsDir)
		if err != nil {
			continue
		}
		for _, t := range taskEntries {
			if !t.IsDir() {
				continue
			}
			taskDir := filepath.Join(wsDir, t.Name())
			usage := buildTaskUsage(taskDir, wsID, t.Name(), matcher)

			report.Tasks = append(report.Tasks, usage)
			report.TotalSizeBytes += usage.SizeBytes
			report.TotalArtifactSizeBytes += usage.ArtifactSizeBytes

			ws, ok := wsAgg[wsID]
			if !ok {
				ws = &WorkspaceDiskUsage{
					WorkspaceID:    wsID,
					WorkspaceShort: ShortID(wsID),
				}
				wsAgg[wsID] = ws
			}
			ws.TaskCount++
			ws.SizeBytes += usage.SizeBytes
			ws.ArtifactSizeBytes += usage.ArtifactSizeBytes
			if usage.AgeSeconds > ws.OldestAgeSeconds {
				ws.OldestAgeSeconds = usage.AgeSeconds
			}
		}
	}

	sort.Slice(report.Tasks, func(i, j int) bool {
		return report.Tasks[i].SizeBytes > report.Tasks[j].SizeBytes
	})

	report.Workspaces = make([]WorkspaceDiskUsage, 0, len(wsAgg))
	for _, ws := range wsAgg {
		ws.ArtifactRatio = ratio(ws.ArtifactSizeBytes, ws.SizeBytes)
		report.Workspaces = append(report.Workspaces, *ws)
	}
	sort.Slice(report.Workspaces, func(i, j int) bool {
		return report.Workspaces[i].SizeBytes > report.Workspaces[j].SizeBytes
	})

	report.TotalTaskCount = len(report.Tasks)
	report.TotalWorkspaceCount = len(report.Workspaces)
	report.TotalArtifactRatio = ratio(report.TotalArtifactSizeBytes, report.TotalSizeBytes)

	return report, nil
}

func ratio(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func buildPatternSet(patterns []string) map[string]struct{} {
	set := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || strings.ContainsAny(p, "/\\") {
			continue
		}
		set[p] = struct{}{}
	}
	return set
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func buildTaskUsage(taskDir, wsID, taskShort string, matcher artifactMatcher) TaskDiskUsage {
	usage := TaskDiskUsage{
		WorkspaceID:    wsID,
		WorkspaceShort: ShortID(wsID),
		TaskShort:      taskShort,
		Path:           taskDir,
		Kind:           DiskUsageKindUnknown,
	}

	metaPresent := false
	if meta, err := execenv.ReadGCMeta(taskDir); err == nil && meta != nil {
		metaPresent = true
		usage.Kind = string(meta.Kind)
		if !meta.CompletedAt.IsZero() {
			usage.AgeSeconds = int64(time.Since(meta.CompletedAt).Seconds())
		} else if age, ok := gcMetaFileAge(taskDir); ok {
			usage.AgeSeconds = int64(age.Seconds())
		}
	}

	if usage.AgeSeconds <= 0 && !metaPresent {
		if info, err := os.Stat(taskDir); err == nil {
			usage.AgeSeconds = int64(time.Since(info.ModTime()).Seconds())
		}
	}

	usage.SizeBytes, usage.ArtifactSizeBytes = taskSize(taskDir, matcher)
	return usage
}

func taskSize(taskDir string, matcher artifactMatcher) (totalBytes int64, artifactBytes int64) {
	if taskDir == "" {
		return
	}
	absRoot, err := filepath.Abs(taskDir)
	if err != nil {
		return
	}

	_ = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == absRoot {
			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if _, ok := matcher.matchDirectory(absRoot, path, entry); ok {
				size := dirSize(path)
				totalBytes += size
				artifactBytes += size
				return filepath.SkipDir
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			totalBytes += info.Size()
		}
		return nil
	})
	return
}

func ShortID(id string) string {
	s := strings.ReplaceAll(id, "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
