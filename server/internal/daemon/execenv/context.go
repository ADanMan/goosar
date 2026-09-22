package execenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	skillpkg "github.com/adanman/goosar/server/internal/skill"
)

const TaskContextMarkerRelPath = ".goosar/daemon_task_context.json"

const TaskContextMarkerManagedBy = "goosar-daemon-task"

type taskContextMarkerFile struct {
	ManagedBy string `json:"managed_by"`
	AgentID   string `json:"agent_id,omitempty"`
	IssueID   string `json:"issue_id,omitempty"`
}

func EnsureWorkspacesRootMarker(workspacesRoot string) error {
	if strings.TrimSpace(workspacesRoot) == "" {
		return errors.New("execenv: workspaces root is required")
	}
	path := filepath.Join(workspacesRoot, TaskContextMarkerRelPath)
	if existing, err := os.ReadFile(path); err == nil {
		var marker taskContextMarkerFile
		if json.Unmarshal(existing, &marker) == nil {
			if marker.ManagedBy == TaskContextMarkerManagedBy {
				return nil
			}

			return fmt.Errorf("foreign file at workspaces root marker path %s; refusing to overwrite", path)
		}

	} else if !os.IsNotExist(err) {

		return fmt.Errorf("read workspaces root marker %s: %w", path, err)
	}
	payload := taskContextMarkerFile{ManagedBy: TaskContextMarkerManagedBy}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspaces root marker: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create workspaces root marker dir: %w", err)
	}
	if err := writeWorkspacesRootMarkerAtomic(path, data); err != nil {
		return fmt.Errorf("write workspaces root marker: %w", err)
	}
	return nil
}

func writeWorkspacesRootMarkerAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".daemon_task_context-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp workspaces root marker: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp workspaces root marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp workspaces root marker: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp workspaces root marker: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename workspaces root marker: %w", err)
	}
	return nil
}

func writeContextFiles(workDir, provider string, ctx TaskContextForEnv, manifest *sidecarManifest) error {
	if err := writeTaskContextMarker(workDir, ctx, manifest); err != nil {
		return err
	}

	contextDir := filepath.Join(workDir, ".agent_context")
	if err := recordMkdirAll(contextDir, 0o755, manifest); err != nil {
		return fmt.Errorf("create .agent_context dir: %w", err)
	}

	content := renderIssueContext(provider, ctx)
	path := filepath.Join(contextDir, "issue_context.md")
	if err := recordWriteFile(path, []byte(content), 0o644, manifest); err != nil {

		if !errors.Is(err, errPathPreExists) {
			return fmt.Errorf("write issue_context.md: %w", err)
		}
	}

	if len(ctx.AgentSkills) > 0 {

		if provider != runtimeCodeJ {
			skillsDir, err := resolveSkillsDir(workDir, provider, manifest)
			if err != nil {
				return fmt.Errorf("resolve skills dir: %w", err)
			}

			if provider != runtimeCodeE {
				if err := writeSkillFiles(skillsDir, ctx.AgentSkills, manifest); err != nil {
					return fmt.Errorf("write skill files: %w", err)
				}
			}
		}
	}

	if err := writeProjectResources(workDir, ctx, manifest); err != nil {

		return fmt.Errorf("write project resources: %w", err)
	}

	return nil
}

func writeTaskContextMarker(workDir string, ctx TaskContextForEnv, manifest *sidecarManifest) error {
	dir := filepath.Dir(filepath.Join(workDir, TaskContextMarkerRelPath))
	if err := recordMkdirAll(dir, 0o755, manifest); err != nil {
		return fmt.Errorf("create .goosar dir: %w", err)
	}

	payload := taskContextMarkerFile{
		ManagedBy: TaskContextMarkerManagedBy,
		AgentID:   ctx.AgentID,
		IssueID:   ctx.IssueID,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task context marker: %w", err)
	}
	if err := recordWriteFile(filepath.Join(workDir, TaskContextMarkerRelPath), data, 0o644, manifest); err != nil {
		if errors.Is(err, errPathPreExists) {
			path := filepath.Join(workDir, TaskContextMarkerRelPath)
			existing, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read existing task context marker: %w", readErr)
			}
			var marker taskContextMarkerFile
			if json.Unmarshal(existing, &marker) != nil || marker.ManagedBy != TaskContextMarkerManagedBy {
				return fmt.Errorf("write task context marker: %w", err)
			}
			if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
				return fmt.Errorf("refresh task context marker: %w", writeErr)
			}
			if manifest != nil {
				manifest.Files = append(manifest.Files, path)
			}
			return nil
		}
		return fmt.Errorf("write task context marker: %w", err)
	}
	return nil
}

type projectResourceFile struct {
	ProjectID          string                  `json:"project_id,omitempty"`
	ProjectTitle       string                  `json:"project_title,omitempty"`
	ProjectDescription string                  `json:"project_description,omitempty"`
	Resources          []ProjectResourceForEnv `json:"resources"`
}

func (p ProjectResourceForEnv) MarshalJSON() ([]byte, error) {
	type alias struct {
		ID           string          `json:"id"`
		ResourceType string          `json:"resource_type"`
		ResourceRef  json.RawMessage `json:"resource_ref"`
		Label        string          `json:"label,omitempty"`
	}
	ref := p.ResourceRef
	if len(ref) == 0 {
		ref = json.RawMessage("{}")
	}
	return json.Marshal(alias{
		ID:           p.ID,
		ResourceType: p.ResourceType,
		ResourceRef:  ref,
		Label:        p.Label,
	})
}

func writeProjectResources(workDir string, ctx TaskContextForEnv, manifest *sidecarManifest) error {
	if ctx.ProjectID == "" && len(ctx.ProjectResources) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, ".goosar", "project")
	if err := recordMkdirAll(dir, 0o755, manifest); err != nil {
		return err
	}
	resources := ctx.ProjectResources
	if resources == nil {
		resources = []ProjectResourceForEnv{}
	}
	payload := projectResourceFile{
		ProjectID:          ctx.ProjectID,
		ProjectTitle:       ctx.ProjectTitle,
		ProjectDescription: ctx.ProjectDescription,
		Resources:          resources,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := recordWriteFile(filepath.Join(dir, "resources.json"), data, 0o644, manifest); err != nil {

		if !errors.Is(err, errPathPreExists) {
			return err
		}
	}
	return nil
}

func resolveSkillsDir(workDir, provider string, manifest *sidecarManifest) (string, error) {
	skillsDir := skillsDirPath(workDir, provider)
	if err := recordMkdirAll(skillsDir, 0o755, manifest); err != nil {
		return "", err
	}
	return skillsDir, nil
}

func skillsDirPath(workDir, provider string) string {
	if d, ok := runtimeDescriptor(provider); ok && d.SkillsWorkdirSubpath != "" {
		return filepath.Join(workDir, filepath.FromSlash(d.SkillsWorkdirSubpath))
	}
	return filepath.Join(workDir, ".agent_context", "skills")
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func ensureSkillFrontmatter(content, slug, description string) string {
	fmStart, ok := frontmatterBodyStart(content)
	if !ok {
		return synthesizeFrontmatter(content, slug, description)
	}

	if hasFrontmatterName(content[fmStart:]) {
		if isFrontmatterValidYAML(content) {
			return content
		}

		_, body, _ := frontmatterParts(content)
		return synthesizeFrontmatter(body, slug, description)
	}

	return content[:fmStart] + "name: " + slug + "\n" + content[fmStart:]
}

func synthesizeFrontmatter(body, slug, description string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", slug)
	if d := strings.TrimSpace(description); d != "" {
		fmt.Fprintf(&b, "description: %s\n", yamlEscapeInline(d))
	}
	b.WriteString("---\n\n")
	b.WriteString(body)
	return b.String()
}

func isFrontmatterValidYAML(content string) bool {
	fmBody, _, ok := frontmatterParts(content)
	if !ok || strings.TrimSpace(fmBody) == "" {
		return false
	}
	var m map[string]any
	return yaml.Unmarshal([]byte(fmBody), &m) == nil
}

func frontmatterParts(content string) (fmBody, body string, ok bool) {
	start, ok := frontmatterBodyStart(content)
	if !ok {
		return "", content, false
	}
	rest := content[start:]
	for searchFrom := 0; ; {
		nl := strings.Index(rest[searchFrom:], "\n---")
		if nl < 0 {
			return "", content, false
		}
		closeAt := searchFrom + nl
		after := rest[closeAt+len("\n---"):]
		switch {
		case after == "" || after == "\r":
			return rest[:closeAt], "", true
		case strings.HasPrefix(after, "\n"):
			return rest[:closeAt], after[len("\n"):], true
		case strings.HasPrefix(after, "\r\n"):
			return rest[:closeAt], after[len("\r\n"):], true
		default:

			searchFrom = closeAt + len("\n---")
		}
	}
}

func frontmatterBodyStart(content string) (int, bool) {
	if strings.HasPrefix(content, "---\n") {
		return 4, true
	}
	if strings.HasPrefix(content, "---\r\n") {
		return 5, true
	}
	return 0, false
}

func hasFrontmatterName(fmBody string) bool {
	closeIdx := strings.Index(fmBody, "\n---")
	if closeIdx < 0 {

		closeIdx = len(fmBody)
	}
	for _, line := range strings.Split(fmBody[:closeIdx], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "name:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		v = strings.Trim(v, `"'`)
		if v != "" {
			return true
		}
	}
	return false
}

func yamlEscapeInline(s string) string {
	flat := strings.ReplaceAll(s, "\r\n", " ")
	flat = strings.ReplaceAll(flat, "\n", " ")
	flat = strings.ReplaceAll(flat, "\r", " ")
	escaped := strings.ReplaceAll(flat, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func sanitizeSkillName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "skill"
	}
	return s
}

func writeSkillFiles(skillsDir string, skills []SkillContextForEnv, manifest *sidecarManifest) error {
	if err := recordMkdirAll(skillsDir, 0o755, manifest); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}

	for _, skill := range skills {
		baseSlug := sanitizeSkillName(skill.Name)
		slug, dir, err := allocateCollisionFreeSkillDir(skillsDir, baseSlug)
		if err != nil {
			return fmt.Errorf("allocate skill dir for %q: %w", skill.Name, err)
		}
		if err := recordMkdirAll(dir, 0o755, manifest); err != nil {
			return err
		}

		body := ensureSkillFrontmatter(skill.Content, slug, skill.Description)
		if err := recordWriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644, manifest); err != nil {
			return err
		}

		for _, f := range skill.Files {
			if skillpkg.IsReservedContentPath(f.Path) {
				continue
			}
			fpath := filepath.Join(dir, f.Path)
			if err := recordMkdirAll(filepath.Dir(fpath), 0o755, manifest); err != nil {
				return err
			}
			if err := recordWriteFile(fpath, []byte(f.Content), 0o644, manifest); err != nil {
				return err
			}
		}
	}

	return nil
}

func renderIssueContext(provider string, ctx TaskContextForEnv) string {
	if ctx.AutopilotRunID != "" {
		return renderAutopilotContext(ctx)
	}
	if ctx.QuickCreatePrompt != "" {
		return renderQuickCreateContext(ctx)
	}

	var b strings.Builder

	b.WriteString("# Task Assignment\n\n")
	fmt.Fprintf(&b, "**Issue ID:** %s\n\n", ctx.IssueID)

	if ctx.TriggerCommentID != "" {
		b.WriteString("**Trigger:** Comment Reply\n")
		b.WriteString("**Triggering comment ID:** `" + ctx.TriggerCommentID + "`\n\n")
	} else {
		b.WriteString("**Trigger:** New Assignment\n\n")
	}

	if ctx.HandoffNote != "" {
		b.WriteString("## Handoff Note\n\n")
		b.WriteString("The person who assigned this issue left this instruction for the run. Treat it as scope guidance and follow it before doing anything broader:\n\n")
		fmt.Fprintf(&b, "> %s\n\n", ctx.HandoffNote)
	}

	b.WriteString("## Quick Start\n\n")
	fmt.Fprintf(&b, "Run `goosar issue get %s --output json` to fetch the full issue details.\n\n", ctx.IssueID)

	skills := modelVisibleSkills(ctx.AgentSkills)
	if len(skills) > 0 {
		b.WriteString("## Agent Skills\n\n")
		b.WriteString("The following skills are available to you:\n\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func renderQuickCreateContext(ctx TaskContextForEnv) string {
	var b strings.Builder
	b.WriteString("# Quick Create\n\n")
	b.WriteString("**Trigger:** Quick-create modal\n\n")
	b.WriteString("## User input\n\n")
	b.WriteString("> ")
	b.WriteString(ctx.QuickCreatePrompt)
	b.WriteString("\n\n")
	skills := modelVisibleSkills(ctx.AgentSkills)
	if len(skills) > 0 {
		b.WriteString("## Agent Skills\n\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderAutopilotContext(ctx TaskContextForEnv) string {
	var b strings.Builder

	b.WriteString("# Autopilot Run\n\n")
	fmt.Fprintf(&b, "**Autopilot run ID:** %s\n\n", ctx.AutopilotRunID)
	if ctx.AutopilotID != "" {
		fmt.Fprintf(&b, "**Autopilot ID:** %s\n\n", ctx.AutopilotID)
	}
	if ctx.AutopilotTitle != "" {
		fmt.Fprintf(&b, "**Title:** %s\n\n", ctx.AutopilotTitle)
	}
	if ctx.AutopilotSource != "" {
		fmt.Fprintf(&b, "**Trigger source:** %s\n\n", ctx.AutopilotSource)
	}
	if ctx.AutopilotTriggerPayload != "" {
		fmt.Fprintf(&b, "## Trigger Payload\n\n```json\n%s\n```\n\n", ctx.AutopilotTriggerPayload)
	}

	b.WriteString("## Quick Start\n\n")
	b.WriteString("This is a run-only autopilot task with no assigned issue. Do not run `goosar issue get` unless the autopilot instructions explicitly ask you to create or update an issue.\n\n")
	if ctx.AutopilotID != "" {
		fmt.Fprintf(&b, "Run `goosar autopilot get %s --output json` if you need the full autopilot configuration.\n\n", ctx.AutopilotID)
	}
	if strings.TrimSpace(ctx.AutopilotDescription) != "" {
		b.WriteString("## Autopilot Instructions\n\n")
		b.WriteString(ctx.AutopilotDescription)
		b.WriteString("\n\n")
	}

	skills := modelVisibleSkills(ctx.AgentSkills)
	if len(skills) > 0 {
		b.WriteString("## Agent Skills\n\n")
		b.WriteString("The following skills are available to you:\n\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	return b.String()
}
