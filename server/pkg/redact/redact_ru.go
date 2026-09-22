package redact

import (
	"regexp"
	"strings"
)

var ruPatterns = []secretPattern{

	{regexp.MustCompile(`(?:\+7|\b8)[ \-]?\(?9\d{2}\)?[ \-]?\d{3}[ \-]?\d{2}[ \-]?\d{2}\b`), "[REDACTED PHONE]"},

	{regexp.MustCompile(`\b\d{3}-\d{3}-\d{3}[ -]\d{2}\b`), "[REDACTED SNILS]"},

	{regexp.MustCompile(`(?i)(^|[^\p{L}])(?:ИНН|INN)[^\d\n]{0,4}(?:\d{12}|\d{10})\b`), "$1[REDACTED INN]"},

	{regexp.MustCompile(`(?i)(^|[^\p{L}])(?:паспорт|passport|серия)[^\d\n]{0,20}\d{4}[^\d\n]{0,20}\d{6}\b`), "$1[REDACTED PASSPORT]"},

	{regexp.MustCompile(`[\p{Cyrillic}A-Za-z0-9._%+-]*\p{Cyrillic}[\p{Cyrillic}A-Za-z0-9._%+-]*@[\p{Cyrillic}A-Za-z0-9.-]+\.[\p{Cyrillic}A-Za-z]{2,}`), "[REDACTED EMAIL]"},

	{regexp.MustCompile(`\bAQVN[A-Za-z0-9_-]{20,}\b`), "[REDACTED YANDEX KEY]"},

	{regexp.MustCompile(`(?i)https?://[a-z0-9.-]+\.bitrix24\.[a-z]+/rest/\d+/[A-Za-z0-9]+/?`), "[REDACTED BITRIX WEBHOOK]"},

	{regexp.MustCompile(`(?i)(^|[^\p{L}])парол[ья][^\n:=]{0,12}[:=]\s*\S+`), "$1[REDACTED PASSWORD]"},

	{regexp.MustCompile(`(?i)\b(?:KRB5_CLIENT_KTNAME|KRB5_KTNAME|KRB5CCNAME|keytab)\s*[=:]\s*\S+`), "[REDACTED KERBEROS]"},
}

var digitRun = regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`)

func redactCards(s string) string {
	return digitRun.ReplaceAllStringFunc(s, func(m string) string {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, m)
		if len(digits) < 13 || len(digits) > 19 || !luhn(digits) {
			return m
		}
		return "[REDACTED CARD]"
	})
}

func luhn(digits string) bool {
	sum, double := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}
