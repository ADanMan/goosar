package daemon

import (
	"fmt"
	"regexp"
	"strings"
)

var verdictPattern = regexp.MustCompile(`(?i)(?:virus|threat|malware)\s*name\s*:?\s*</b>\s*([A-Za-z0-9._-]+)`)

func looksLikeHTML(body string) bool {
	head := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

func describeInterceptedResponse(body string) (string, bool) {
	if !looksLikeHTML(body) {
		return "", false
	}

	match := verdictPattern.FindStringSubmatch(body)
	if len(match) != 2 {
		return "the network route answered with a security-gateway page instead of the server", true
	}
	return fmt.Sprintf(
		"the network route answered with a security-gateway page instead of the server; the gateway blocked the download with the verdict %s",
		match[1],
	), true
}
