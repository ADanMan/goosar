package skill

import "strings"

// Лимиты импорта — contract §2 "Импорт навыков — источники и лимиты".
const (
	MaxImportFiles      = 256
	MaxImportFileBytes  = 1 << 20  // 1 МБ на файл
	MaxImportTotalBytes = 8 << 20  // 8 МБ суммарно
	MaxUploadBytes      = 16 << 20 // 16 МБ на сам multipart-запрос до распаковки
	ImportTimeoutSecs   = 45
)

// binaryExtensions — "бинарные" расширения, пропускаемые молча при импорте
// (изображения, шрифты, архивы, документы Office, аудио/видео, исполняемые
// файлы, базы данных) — contract перечисляет категории, не точный список;
// решение этой сессии — конкретный список ниже (server2/docs/decisions.md).
var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".ico": true, ".svg": true,
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true,
	".zip": true, ".tar": true, ".gz": true, ".tgz": true, ".7z": true, ".rar": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true, ".pdf": true,
	".mp3": true, ".wav": true, ".ogg": true, ".mp4": true, ".mov": true, ".avi": true, ".webm": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
	".db": true, ".sqlite": true, ".sqlite3": true,
}

func isBinaryPath(p string) bool {
	i := strings.LastIndex(p, ".")
	if i < 0 {
		return false
	}
	return binaryExtensions[strings.ToLower(p[i:])]
}

// isLicensePath — LICENSE/LICENSE.md/LICENSE.txt (в любом регистре, на
// любой глубине пути) — пропускаются молча.
func isLicensePath(p string) bool {
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	switch strings.ToUpper(base) {
	case "LICENSE", "LICENSE.MD", "LICENSE.TXT":
		return true
	}
	return false
}

// Source — один из разрешённых доменов импорта (contract §2).
type Source string

const (
	SourceClawhub  Source = "clawhub.ai"
	SourceSkillsSH Source = "skills.sh"
	SourceGitHub   Source = "github.com"
)

// allowedSourceHosts — какие source этот сервер вообще распознаёт по
// домену. Отключение конкретного источника администратором деплоя
// (contract: "по отдельности") — platform_policy.pp_body (T-029, деплой-
// уровень), которого в этой сессии ещё нет структурировано под skills;
// решение — читать необязательный ключ `pp_body->>'skills_source_<host>_disabled'`
// при наличии строки platform_policy (id=1), иначе считать все включёнными
// (см. server2/docs/decisions.md).
var allowedSourceHosts = map[string]Source{
	"clawhub.ai":     SourceClawhub,
	"skills.sh":      SourceSkillsSH,
	"github.com":     SourceGitHub,
	"www.github.com": SourceGitHub,
}
