package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type localPathLinkFinding struct {
	Target string
	Reason string
}

func findLocalPathLinks(body string) []localPathLinkFinding {
	source := []byte(body)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))

	var findings []localPathLinkFinding
	seen := make(map[string]struct{})
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var target string
		switch node := n.(type) {
		case *ast.Link:
			target = string(node.Destination)
		case *ast.Image:
			target = string(node.Destination)
		case *ast.AutoLink:

			target = string(node.URL(source))
		default:
			return ast.WalkContinue, nil
		}

		reason := classifyLocalPathTarget(target)
		if reason == "" {
			return ast.WalkContinue, nil
		}
		if _, dup := seen[target]; dup {
			return ast.WalkContinue, nil
		}
		seen[target] = struct{}{}
		findings = append(findings, localPathLinkFinding{Target: target, Reason: reason})
		return ast.WalkContinue, nil
	})
	return findings
}

func classifyLocalPathTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}

	if filepath.IsAbs(target) {
		if within, err := fileWithinWorkingDir(target); err == nil && within {
			return "it is inside this task's working directory"
		}
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			return "it names a file that exists only on this machine"
		}
		return ""
	}

	if parsed, err := url.Parse(target); err == nil && strings.EqualFold(parsed.Scheme, "file") {
		return "it is a file:// URL"
	}
	return ""
}

func guardLocalPathLinks(body, field, deliveryHint string) error {
	if !inAgentExecutionContext() {
		return nil
	}
	findings := findLocalPathLinks(body)
	if len(findings) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s links %d runtime-local path(s), which no reader can open:\n", field, len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "  - %q — %s\n", f.Target, f.Reason)
	}
	b.WriteString("\nThe path exists only on the machine running you; for everyone else the link is dead. ")
	b.WriteString(deliveryHint)
	b.WriteString("\nTo merely reference a code location, use inline code instead of a link (`path/to/file.ts:42`) — code spans and fenced blocks are not checked.")
	return fmt.Errorf("%s", b.String())
}
