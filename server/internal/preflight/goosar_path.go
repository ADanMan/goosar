package preflight

import (
	"context"
	"path/filepath"
)

type SelfInfo struct {
	Path    string
	Version string
}

func checkGoosarOnPath(ctx context.Context, opts Options) Result {
	res := Result{
		ID:      "goosar",
		Name:    "goosar в PATH",
		Message: "Терминал должен находить тот же goosar, что встроен в приложение.",
		Fix: "В приложении: меню Справка → «Установить командную строку». Без приложения: " +
			"curl -fsSL <адрес стенда>/install.sh | bash -s -- --app-url … --server-url … (SELF_HOSTING.md).",
	}
	if opts.Self == nil || opts.Self.Path == "" {
		res.Status = StatusSkipped
		res.Message = "Собственный путь goosar неизвестен — проверка пропущена."
		return res
	}
	found, err := opts.lookup()("goosar")
	if err != nil {
		res.Status = StatusMissing
		res.Message = "goosar не найден в PATH: команды в терминале работать не будут."
		return res
	}
	res.Path = found
	res.Detail = found
	if sameFile(found, opts.Self.Path) {
		res.Status = StatusOK
		res.Version = opts.Self.Version
		return res
	}

	version := normalizeVersion(firstVersion(ctx, opts, found))
	self := normalizeVersion(opts.Self.Version)
	res.Version = version
	if versionPattern.MatchString(version) && versionPattern.MatchString(self) && version != self {
		res.Status = StatusOutdated
		res.Message = "В PATH другой goosar (" + version + "), приложение встроило " + opts.Self.Version +
			": терминал и приложение будут вести себя по-разному."
		res.Fix = "Уберите старый бинарь (" + found + ") или переустановите командную строку из меню приложения."
		return res
	}
	res.Status = StatusOK
	res.Detail = found + " (другой файл той же версии)"
	return res
}

func sameFile(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return ra == rb
}
