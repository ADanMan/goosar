package util

import (
	"strings"
	"unicode/utf8"
)

func UnescapeBackslashEscapes(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			case 't':
				b.WriteByte('\t')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

const sanitizeJSONMaxDepth = 32

func SanitizeTextForPostgres(s string) string {

	if !strings.ContainsRune(s, 0) && utf8.ValidString(s) {
		return s
	}
	return strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", "")
}

func SanitizeJSONForPostgres(v any) any {
	return sanitizeJSONValue(v, 0)
}

func sanitizeJSONValue(v any, depth int) any {
	if depth > sanitizeJSONMaxDepth {
		return nil
	}
	switch t := v.(type) {
	case string:
		return SanitizeTextForPostgres(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[SanitizeTextForPostgres(k)] = sanitizeJSONValue(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = sanitizeJSONValue(val, depth+1)
		}
		return out
	default:

		return v
	}
}
