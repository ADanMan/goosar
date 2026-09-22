package skill

import (
	"path/filepath"
	"strings"
)

const ContentFilename = "SKILL.md"

func IsReservedContentPath(p string) bool {
	return strings.EqualFold(filepath.Clean(p), ContentFilename)
}
