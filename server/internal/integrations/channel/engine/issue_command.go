package engine

import "strings"

const issueCommandPrefix = "/issue"

func ParseIssueCommand(body string) (*IssueCommand, bool) {
	lines := strings.Split(body, "\n")

	firstIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			firstIdx = i
			break
		}
	}
	if firstIdx == -1 {
		return nil, false
	}

	trimmed := strings.TrimLeft(lines[firstIdx], " \t")
	if !strings.HasPrefix(trimmed, issueCommandPrefix) {
		return nil, false
	}

	rest := trimmed[len(issueCommandPrefix):]
	if rest != "" {
		if r0 := rest[0]; r0 != ' ' && r0 != '\t' {
			return nil, false
		}
	}

	title := strings.TrimSpace(rest)
	description := ""
	if firstIdx+1 < len(lines) {
		description = strings.TrimRight(strings.Join(lines[firstIdx+1:], "\n"), " \t\n")
	}
	return &IssueCommand{Title: title, Description: description}, true
}

func titleFromPreviousMessage(body string) string {
	if cmd, ok := ParseIssueCommand(body); ok {
		return cmd.Title
	}
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
