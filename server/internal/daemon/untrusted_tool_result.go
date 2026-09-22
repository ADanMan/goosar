package daemon

import (
	"regexp"
	"strings"
)

const (
	untrustedToolOpenTag  = "<untrusted-mcp-tool-result>"
	untrustedToolCloseTag = "</untrusted-mcp-tool-result>"

	mcpToolNamePrefix = "mcp__"
)

var untrustedToolDelimiterRE = regexp.MustCompile(`(?i)</?untrusted-mcp-tool-result>`)

func isMCPToolName(tool string) bool {
	rest, hasPrefix := strings.CutPrefix(strings.ToLower(tool), mcpToolNamePrefix)
	if !hasPrefix {
		return false
	}
	server, name, hasSeparator := strings.Cut(rest, "__")
	return hasSeparator && server != "" && name != ""
}

func escapeUntrustedToolDelimiters(s string) string {
	if !untrustedToolDelimiterRE.MatchString(s) {
		return s
	}
	return untrustedToolDelimiterRE.ReplaceAllStringFunc(s, escapeAngleBrackets)
}

func escapeAngleBrackets(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 6)
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func wrapUntrustedToolOutput(tool, output string) string {
	if output == "" || !isMCPToolName(tool) {
		return output
	}
	return untrustedToolOpenTag + escapeUntrustedToolDelimiters(output) + untrustedToolCloseTag
}
