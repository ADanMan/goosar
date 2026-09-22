package service

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/adanman/goosar/server/internal/util"
)

const (
	maxSkillBodyLines = 500

	maxDescriptionChars = 1024
)

func TestBuiltinSkillsConformToTemplate(t *testing.T) {
	skills := loadBuiltinSkills()
	if len(skills) == 0 {
		t.Fatal("no built-in skills loaded; embed or layout is broken")
	}

	for _, skill := range skills {
		t.Run(skill.Name, func(t *testing.T) {

			if !strings.HasPrefix(skill.Name, "goosar-") {
				t.Errorf("skill name %q must carry the goosar- prefix", skill.Name)
			}

			fm, body, ok := splitFrontmatter(skill.Content)
			if !ok {
				t.Fatalf("SKILL.md must lead with a --- frontmatter block")
			}
			if strings.TrimSpace(fm["name"]) == "" {
				t.Errorf("frontmatter is missing a non-empty name")
			}
			desc := strings.TrimSpace(fm["description"])
			if desc == "" {
				t.Errorf("frontmatter is missing a description (the only thing an agent sees when deciding to load the skill)")
			}
			if len(desc) > maxDescriptionChars {
				t.Errorf("description is %d chars, over the %d cap", len(desc), maxDescriptionChars)
			}
			if n := strings.Count(body, "\n") + 1; n > maxSkillBodyLines {
				t.Errorf("SKILL.md body is %d lines, over the %d-line L2 budget; move detail into one-level-deep supporting files", n, maxSkillBodyLines)
			}

			for _, f := range skill.Files {
				lower := strings.ToLower(f.Path)
				if strings.Contains(lower, "eval") || strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, "_test.md") {
					t.Errorf("supporting file %q looks like an eval/test; evals belong in _test.go, not the shipped skill payload", f.Path)
				}
			}
		})
	}
}

func TestBuiltinSkillsFrontmatterIsStrictYAML(t *testing.T) {
	skills := loadBuiltinSkills()
	if len(skills) == 0 {
		t.Fatal("no built-in skills loaded; embed or layout is broken")
	}

	for _, skill := range skills {
		t.Run(skill.Name, func(t *testing.T) {
			content := skill.Content
			if !strings.HasPrefix(content, "---\n") {
				t.Fatalf("SKILL.md must lead with a --- frontmatter block")
			}
			rest := content[len("---\n"):]
			end := strings.Index(rest, "\n---")
			if end < 0 {
				t.Fatalf("frontmatter has no closing --- delimiter")
			}

			var fm map[string]any
			if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
				t.Fatalf("frontmatter is not valid YAML — a strict runtime (e.g. Codex) "+
					"will drop this skill on load; quote values containing ': ': %v", err)
			}

			if name, ok := fm["name"].(string); !ok || strings.TrimSpace(name) == "" {
				t.Errorf("frontmatter name must parse as a non-empty string, got %#v", fm["name"])
			}
			if desc, ok := fm["description"].(string); !ok || strings.TrimSpace(desc) == "" {
				t.Errorf("frontmatter description must parse as a non-empty string, got %#v", fm["description"])
			}
		})
	}
}

func TestMentioningSkillFollowsContractFrontmatter(t *testing.T) {
	skill, ok := findSkill(t, "goosar-mentioning")
	if !ok {
		return
	}
	fm, _, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (a platform-contract skill triggers from context, not a slash command)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); got != "Bash(goosar *)" {
		t.Errorf("allowed-tools = %q, want Bash(goosar *) (fence the skill to the CLI it teaches)", got)
	}
}

func TestMentioningSkillTeachesTheParserContract(t *testing.T) {
	const uuid = "7f3a1b2c-0000-4000-8000-000000000abc"

	cases := []struct {
		name    string
		content string
		want    []util.Mention
	}{
		{

			name:    "name where a uuid belongs is silently dead",
			content: "[@Alice](mention://member/Alice) please review",
			want:    nil,
		},
		{

			name:    "bare @name is plain text",
			content: "@alice please review",
			want:    nil,
		},
		{

			name:    "real uuid with matching type fires",
			content: "[@Alice](mention://member/" + uuid + ") please review",
			want:    []util.Mention{{Type: "member", ID: uuid}},
		},
		{

			name:    "all uses the literal all",
			content: "[@all](mention://all/all) heads up",
			want:    []util.Mention{{Type: "all", ID: "all"}},
		},
		{

			name:    "wrong type still parses (points at wrong entity)",
			content: "[@Bot](mention://member/" + uuid + ")",
			want:    []util.Mention{{Type: "member", ID: uuid}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := util.ParseMentions(tc.content)
			if len(got) != len(tc.want) {
				t.Fatalf("ParseMentions(%q) = %+v, want %+v", tc.content, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("mention[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestWorkingOnIssuesSkillCoversIssueLoopContracts(t *testing.T) {
	skill, ok := findSkill(t, "goosar-working-on-issues")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (issue workflow guidance triggers from context)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(goosar *)") {
		t.Errorf("allowed-tools = %q, want access to the Goosar CLI", got)
	}

	mustContain := []string{
		"goosar issue pull-requests <issue-id> --output json",
		"Default for code-changing issue work",
		"open or update a PR before posting the final Goosar issue comment",
		"This is a default, not",
		"Use a routable issue key in the PR title, body, or branch",
		"include the PR URL when a PR exists",
		"Closes MUL-2759",
		"--status backlog",
		"pr_url",
		"references/working-on-issues-source-map.md",

		"There is no `goosar issue comment create`",
		"goosar issue comment add <issue-id> --content-file ./reply.md",
		"A bare `--content-stdin` gets no body from",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("working-on-issues skill missing %q", want)
		}
	}

	mustNotContain := []string{
		"Start from the trigger, not from memory",
		"goosar issue get <issue-id> --output json",
		"goosar issue metadata list <issue-id> --output json",
		"goosar issue comment list <issue-id> --thread <trigger-comment-id>",
		"goosar issue comment add <issue-id> --parent <trigger-comment-id>",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(body, forbidden) {
			t.Errorf("working-on-issues skill duplicates runtime prompt contract %q", forbidden)
		}
	}

	if !skillHasFile(skill, "references/working-on-issues-source-map.md") {
		t.Errorf("working-on-issues skill missing supporting file references/working-on-issues-source-map.md")
	}
}

func TestSkillImportingSkillCoversWorkspaceImportContracts(t *testing.T) {
	skill, ok := findSkill(t, "goosar-skill-importing")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (skill import guidance triggers from context)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(goosar *)") {
		t.Errorf("allowed-tools = %q, want access to the Goosar CLI", got)
	}

	mustContain := []string{
		"goosar skill import --url <url> --output json",
		"/api/skills/import",
		"clawhub.ai",
		"skills.sh",
		"github.com",
		"config.origin",
		"--on-conflict fail",
		"--on-conflict overwrite",
		"--on-conflict rename",
		"--on-conflict skip",
		"status",
		"conflict",
		"skipped",
		"409",
		"existing_skill",
		"id",
		"name",
		"legacy",
		"goosar skill list --output json",
		"npx skills add",
		"goosar agent skills add <agent-id> --skill-ids <skill-id> --output json",
		"goosar agent skills list <agent-id> --output json",
		"replace-all",
		"`set` is the replacement path",
		"references/skill-importing-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("skill-importing skill missing %q", want)
		}
	}

	mustNotContain := []string{
		"goosar agent skills set <agent-id> --skill-ids <skill-id>",
		"merge the new skill id with the existing ids",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(body, forbidden) {
			t.Errorf("skill-importing skill should not teach stale or destructive binding command %q", forbidden)
		}
	}

	if !skillHasFile(skill, "references/skill-importing-source-map.md") {
		t.Errorf("skill-importing skill missing supporting file references/skill-importing-source-map.md")
	}
}

func TestCreatingAgentsSkillCoversAgentCreationContracts(t *testing.T) {
	skill, ok := findSkill(t, "goosar-creating-agents")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (agent creation guidance triggers from context)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(goosar *)") {
		t.Errorf("allowed-tools = %q, want access to the Goosar CLI", got)
	}

	mustContain := []string{
		"not a parameter manual",
		"`description` is a catalog summary",
		"`instructions` is the runtime behavior contract",
		"`avatar_url` → a random `emoji:<glyph>`",
		"goosar agent create --name <name> --runtime-id <runtime-id>",
		"`model` is a first-class persisted column",
		"custom_env",
		"--custom-env-stdin",
		"--custom-env-file",
		"goosar agent skills add <agent-id> --skill-ids <skill-id> --output json",
		"goosar agent skills list <agent-id> --output json",
		"goosar agent get <agent-id> --output json",
		"255",
		"references/creating-agents-source-map.md",
		"Never put credentials or other secrets in `custom_args`",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("creating-agents skill missing %q", want)
		}
	}

	mustNotContain := []string{
		"--from-template",
		"/api/agent-templates",
		"template_slug",
		"curated template",
		"copy this parameter list",

		"Define the job first",
		"Run a low-risk task",
		"Decision flow",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(body, forbidden) {
			t.Errorf("creating-agents skill should not teach immature template content or generic how-to coaching %q", forbidden)
		}
	}

	if !skillHasFile(skill, "references/creating-agents-source-map.md") {
		t.Errorf("creating-agents skill missing supporting file references/creating-agents-source-map.md")
	}
}

func TestSquadsSkillCoversLeaderRoutingContract(t *testing.T) {
	skill, ok := findSkill(t, "goosar-squads")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (squad guidance triggers from context)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(goosar *)") {
		t.Errorf("allowed-tools = %q, want access to the Goosar CLI", got)
	}

	mustContain := []string{
		"A squad is not an agent",
		"squad's `leader_id` agent",
		"squad members are not automatically fanned out",
		"goosar squad member set-role",
		"mention://squad/<squad-id>",
		"recording squad activity",
		"references/squad-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("squads skill missing %q", want)
		}
	}

	if !skillHasFile(skill, "references/squad-source-map.md") {
		t.Errorf("squads skill missing supporting file references/squad-source-map.md")
	}
}

func TestAutopilotsSkillCoversDispatchAndSideEffects(t *testing.T) {
	skill, ok := findSkill(t, "goosar-autopilots")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(goosar *)") {
		t.Errorf("allowed-tools = %q, want access to the Goosar CLI", got)
	}

	mustContain := []string{
		"An autopilot is not an agent",
		"create_issue",
		"run_only",
		"goosar autopilot trigger-add <autopilot-id> --kind schedule",
		"goosar autopilot trigger <autopilot-id> --output json",
		"Do not run `trigger`",
		"webhook tokens",
		"{{date}}",
		"squad's leader agent",
		"references/autopilots-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("autopilots skill missing %q", want)
		}
	}
	if !skillHasFile(skill, "references/autopilots-source-map.md") {
		t.Errorf("autopilots skill missing supporting file references/autopilots-source-map.md")
	}
}

func TestRuntimesAndReposSkillCoversClaimAndCheckoutChain(t *testing.T) {
	skill, ok := findSkill(t, "goosar-runtimes-and-repos")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(goosar *)") {
		t.Errorf("allowed-tools = %q, want access to the Goosar CLI", got)
	}

	mustContain := []string{
		"agent_task_queue",
		"daemon polls/claims the task",
		"goosar runtime list --output json",
		"goosar repo checkout <url>",
		"GOOSAR_DAEMON_PORT",
		"resource_ref.ref",
		"github_repo",
		"local_directory",
		"Runtime and repo commands affect active agent execution",
		"references/runtimes-and-repos-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("runtimes-and-repos skill missing %q", want)
		}
	}
	if !skillHasFile(skill, "references/runtimes-and-repos-source-map.md") {
		t.Errorf("runtimes-and-repos skill missing supporting file references/runtimes-and-repos-source-map.md")
	}
}

func TestProjectsAndResourcesSkillCoversDurableContext(t *testing.T) {
	skill, ok := findSkill(t, "goosar-projects-and-resources")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(goosar *)") {
		t.Errorf("allowed-tools = %q, want access to the Goosar CLI", got)
	}

	mustContain := []string{
		"Projects are durable context containers",
		".goosar/project/resources.json",
		"goosar project resource list <project-id> --output json",
		"goosar project resource add <project-id> --type github_repo --url <github-url> --output json",
		"goosar project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json",
		"goosar project resource add <project-id> --type local_directory",
		"Project resources are durable and affect future tasks",
		"github_repo.resource_ref.url",
		"resource_ref.ref",
		"references/projects-and-resources-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("projects-and-resources skill missing %q", want)
		}
	}
	if !skillHasFile(skill, "references/projects-and-resources-source-map.md") {
		t.Errorf("projects-and-resources skill missing supporting file references/projects-and-resources-source-map.md")
	}
}

func TestOnboardingSkillCoversFirstSessionMethod(t *testing.T) {
	skill, ok := findSkill(t, "goosar-onboarding")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (the first session triggers from context, not a slash command)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(goosar *)") {
		t.Errorf("allowed-tools = %q, want access to the Goosar CLI", got)
	}

	mustContain := []string{

		"one real intention of theirs turned into one running",
		"Do not greet again",
		"At most **one** clarifying question",
		"Prefer proposing a default over asking",
		"time zone",
		"goosar issue create",
		"--description-file",
		"enqueues that agent immediately",
		"`--status backlog` deliberately does NOT enqueue",
		"Create as little as possible",
		"Preview, then create",
		"Never promise to come back",
		"Never ask for credentials",

		"goosar status --output json",

		"/<workspace-slug>/capabilities",
		"goosar workspace get --output json",
		"references/onboarding-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("onboarding skill missing %q", want)
		}
	}

	mustNotContain := []string{

		"I'll get back to you with",
		"once it is finished I will",

		"ask them for their API key",
		"paste your token",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(body, forbidden) {
			t.Errorf("onboarding skill teaches a dishonest or unsafe move: %q", forbidden)
		}
	}

	if !skillHasFile(skill, "references/onboarding-source-map.md") {
		t.Errorf("onboarding skill missing supporting file references/onboarding-source-map.md")
	}
}

func findSkill(t *testing.T, name string) (AgentSkillData, bool) {
	t.Helper()
	for _, s := range loadBuiltinSkills() {
		if s.Name == name {
			return s, true
		}
	}
	t.Errorf("built-in skill %q not found", name)
	return AgentSkillData{}, false
}

func skillHasFile(skill AgentSkillData, path string) bool {
	for _, f := range skill.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

func splitFrontmatter(content string) (map[string]string, string, bool) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, content, false
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, content, false
	}
	block := rest[:end]
	body := rest[end:]
	if nl := strings.Index(body, "\n"); nl >= 0 {
		body = body[nl+1:]
	}

	fm := make(map[string]string)
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fm[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"'`)
	}
	return fm, body, true
}
