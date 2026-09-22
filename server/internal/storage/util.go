package storage

import (
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

func sanitizeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {

		if r < 0x20 || r == 0x7f || r == '"' || r == ';' || r == '\\' || r == '\x00' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func asciiOnlyFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r > 0x7f {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func needsRFC5987Encoding(name string) bool {
	for _, r := range name {
		if r > 0x7f {
			return true
		}
	}
	return false
}

func rfc5987Encode(name string) string {
	var b strings.Builder
	b.Grow(len(name) * 3)
	for _, r := range name {
		if r <= 0x7f && isAttrChar(byte(r)) {
			b.WriteByte(byte(r))
		} else {

			buf := make([]byte, 4)
			n := utf8.EncodeRune(buf, r)
			for i := 0; i < n; i++ {
				b.WriteByte('%')
				b.WriteString(strings.ToUpper(hex.EncodeToString(buf[i : i+1])))
			}
		}
	}
	return b.String()
}

func isAttrChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		strings.ContainsRune("!#$&+-.^_`|~", rune(c))
}

func ContentDisposition(contentType, filename string) string {
	disposition := "attachment"
	if isInlineContentType(contentType) {
		disposition = "inline"
	}
	if !needsRFC5987Encoding(filename) {
		return disposition + `; filename="` + sanitizeFilename(filename) + `"`
	}

	asciiFallback := sanitizeFilename(asciiOnlyFilename(filename))
	return disposition + `; filename="` + asciiFallback + `"; filename*=UTF-8''` + rfc5987Encode(filename)
}

func AttachmentContentDisposition(filename string) string {
	if !needsRFC5987Encoding(filename) {
		return `attachment; filename="` + sanitizeFilename(filename) + `"`
	}
	asciiFallback := sanitizeFilename(asciiOnlyFilename(filename))
	return `attachment; filename="` + asciiFallback + `"; filename*=UTF-8''` + rfc5987Encode(filename)
}

func isInlineContentType(ct string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	if mediaType == "image/svg+xml" {
		return false
	}
	return strings.HasPrefix(mediaType, "image/") ||
		strings.HasPrefix(mediaType, "video/") ||
		strings.HasPrefix(mediaType, "audio/") ||
		mediaType == "application/pdf"
}
