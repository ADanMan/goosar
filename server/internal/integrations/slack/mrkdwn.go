package slack

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reFenced      = regexp.MustCompile("(?s)(```(?:[^\\n]*\\n)?.*?```)")
	reInlineCode  = regexp.MustCompile("(`[^`]+`)")
	reMdLink      = regexp.MustCompile(`(!?)\[([^\]]+)\]\(([^()]*(?:\([^()]*\)[^()]*)*)\)`)
	reSlackEntity = regexp.MustCompile(`(<(?:[@#!]|(?:https?|mailto|tel):)[^>\n]+>)`)
	reBlockquote  = regexp.MustCompile(`(?m)^(>+\s)`)
	reHeader      = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reInnerBold   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldItalic  = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reBold        = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic      = regexp.MustCompile(`\*(\S(?:[^*\n]*?\S)?)\*`)
	reStrike      = regexp.MustCompile(`~~(.+?)~~`)
)

func formatMrkdwn(content string) string {
	if content == "" {
		return content
	}
	p := &mrkdwnPlaceholders{values: map[string]string{}}
	text := content

	text = reFenced.ReplaceAllStringFunc(text, p.stash)
	text = reInlineCode.ReplaceAllStringFunc(text, p.stash)

	text = reMdLink.ReplaceAllStringFunc(text, func(m string) string {
		sub := reMdLink.FindStringSubmatch(m)
		if sub[1] == "!" {
			return m
		}
		url := strings.TrimSpace(sub[3])
		if strings.HasPrefix(url, "<") && strings.HasSuffix(url, ">") {
			url = strings.TrimSpace(url[1 : len(url)-1])
		}
		return p.stash("<" + url + "|" + sub[2] + ">")
	})

	text = reSlackEntity.ReplaceAllStringFunc(text, p.stash)
	text = reBlockquote.ReplaceAllStringFunc(text, p.stash)

	text = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(text)
	text = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)

	text = reHeader.ReplaceAllStringFunc(text, func(m string) string {
		inner := strings.TrimSpace(reHeader.FindStringSubmatch(m)[1])
		inner = reInnerBold.ReplaceAllString(inner, "$1")
		return p.stash("*" + inner + "*")
	})

	text = reBoldItalic.ReplaceAllStringFunc(text, func(m string) string {
		return p.stash("*_" + reBoldItalic.FindStringSubmatch(m)[1] + "_*")
	})
	text = reBold.ReplaceAllStringFunc(text, func(m string) string {
		return p.stash("*" + reBold.FindStringSubmatch(m)[1] + "*")
	})
	text = reItalic.ReplaceAllStringFunc(text, func(m string) string {
		return p.stash("_" + reItalic.FindStringSubmatch(m)[1] + "_")
	})
	text = reStrike.ReplaceAllStringFunc(text, func(m string) string {
		return p.stash("~" + reStrike.FindStringSubmatch(m)[1] + "~")
	})

	for i := len(p.order) - 1; i >= 0; i-- {
		k := p.order[i]
		text = strings.ReplaceAll(text, k, p.values[k])
	}
	return text
}

type mrkdwnPlaceholders struct {
	values map[string]string
	order  []string
	n      int
}

func (p *mrkdwnPlaceholders) stash(v string) string {
	key := "\x00SL" + strconv.Itoa(p.n) + "\x00"
	p.n++
	p.values[key] = v
	p.order = append(p.order, key)
	return key
}
