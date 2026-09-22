package execenv

type taskKind int

const (
	kindIssue taskKind = iota

	kindAutopilotRunOnly

	kindQuickCreate

	kindChat
)

func classifyTask(ctx TaskContextForEnv) taskKind {
	if ctx.ChatSessionID != "" {
		return kindChat
	}
	if ctx.QuickCreatePrompt != "" {
		return kindQuickCreate
	}
	if ctx.AutopilotRunID != "" {
		return kindAutopilotRunOnly
	}
	return kindIssue
}

func (k taskKind) hasIssueContext() bool {
	return k == kindIssue
}
