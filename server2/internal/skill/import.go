package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/adanman/goosar/server2/internal/store"
)

// ErrSkillMDMissing — SKILL.md отсутствует или пуст (contract: отклоняется
// как ошибка).
var ErrSkillMDMissing = errors.New("skill: SKILL.md отсутствует или пуст")

// ErrSourceDisabled — источник импорта отключён администратором деплоя.
var ErrSourceDisabled = errors.New("skill: источник импорта отключён на этом деплое")

// ErrSourceUnsupported — домен ссылки не входит в разрешённый список.
var ErrSourceUnsupported = errors.New("skill: неподдерживаемый источник")

// ErrTooManyFiles / ErrTooLarge — превышены лимиты §2.
var ErrTooManyFiles = errors.New("skill: превышен лимит числа файлов бандла")
var ErrTooLarge = errors.New("skill: превышен лимит размера бандла")

// ExistingIdentity — components/schemas/ExistingSkillIdentity.
type ExistingIdentity struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CreatedBy    string `json:"created_by"`
	CanOverwrite bool   `json:"can_overwrite"`
}

// Result — components/schemas/SkillImportResult.
type Result struct {
	Status        string            `json:"status"` // created|updated|skipped|conflict|failed
	Reason        string            `json:"reason,omitempty"`
	Skill         map[string]any    `json:"skill,omitempty"`
	ExistingSkill *ExistingIdentity `json:"existing_skill,omitempty"`
}

// bundleEntry — один файл распакованного/полученного бандла до записи в БД.
type bundleEntry struct {
	Path    string
	Content string
}

// extractBundle применяет общие правила фильтрации/лимитов §2 к сырому
// списку файлов источника (zip-записи или github contents) и выделяет
// SKILL.md отдельно.
func extractBundle(entries []bundleEntry) (skillMD string, files []FileInput, err error) {
	if len(entries) > MaxImportFiles {
		return "", nil, ErrTooManyFiles
	}
	var total int64
	var mdFound bool
	for _, e := range entries {
		if isBinaryPath(e.Path) || isLicensePath(e.Path) {
			continue
		}
		if err := ValidatePath(e.Path); err != nil {
			continue // источник может содержать записи вне контроля пользователя; пропускаем как непригодные, не отклоняем всё
		}
		if int64(len(e.Content)) > MaxImportFileBytes {
			return "", nil, ErrTooLarge
		}
		total += int64(len(e.Content))
		if total > MaxImportTotalBytes {
			return "", nil, ErrTooLarge
		}
		base := path.Base(e.Path)
		if base == ReservedFilePath {
			skillMD = e.Content
			mdFound = true
			continue
		}
		files = append(files, FileInput{Path: e.Path, Content: e.Content})
	}
	if !mdFound || strings.TrimSpace(skillMD) == "" {
		return "", nil, ErrSkillMDMissing
	}
	return skillMD, files, nil
}

// FromZip разбирает архив (ZIP/.skill) согласно §2 и подготавливает бандл к
// импорту (имя/описание — из фронтматтера SKILL.md, если есть).
func FromZip(data []byte) (name, description, content string, files []FileInput, err error) {
	if int64(len(data)) > MaxUploadBytes {
		return "", "", "", nil, ErrTooLarge
	}
	zr, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if zerr != nil {
		return "", "", "", nil, fmt.Errorf("skill: архив повреждён: %w", zerr)
	}
	var entries []bundleEntry
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			return "", "", "", nil, fmt.Errorf("skill: чтение файла архива: %w", oerr)
		}
		body, rerr := io.ReadAll(io.LimitReader(rc, MaxImportFileBytes+1))
		rc.Close()
		if rerr != nil {
			return "", "", "", nil, fmt.Errorf("skill: чтение файла архива: %w", rerr)
		}
		entries = append(entries, bundleEntry{Path: stripCommonPrefix(f.Name), Content: string(body)})
	}
	skillMD, fileInputs, err := extractBundle(entries)
	if err != nil {
		return "", "", "", nil, err
	}
	fmName, fmDesc, body := parseFrontmatter(skillMD)
	if fmName == "" {
		fmName = deriveNameFromZip(zr)
	}
	return fmName, fmDesc, body, fileInputs, nil
}

// stripCommonPrefix — многие архивы навыков кладут всё под одной корневой
// директорией (owner-slug-hash/...); отбрасываем один верхний уровень, если
// он есть, чтобы capf_path не содержал бессмысленный общий префикс.
func stripCommonPrefix(p string) string {
	if i := strings.Index(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func deriveNameFromZip(zr *zip.Reader) string {
	if len(zr.File) == 0 {
		return "imported-skill"
	}
	first := zr.File[0].Name
	if i := strings.Index(first, "/"); i >= 0 {
		return first[:i]
	}
	return "imported-skill"
}

// resolveSource определяет источник ссылки (contract §2): домен из списка,
// либо "owner/slug" без схемы — трактуется как ссылка на clawhub.ai.
func resolveSource(rawURL string) (Source, string, error) {
	if !strings.Contains(rawURL, "://") {
		if strings.Count(rawURL, "/") == 1 && !strings.HasPrefix(rawURL, "/") {
			return SourceClawhub, "https://clawhub.ai/" + rawURL, nil
		}
		return "", "", ErrSourceUnsupported
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("skill: некорректный url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	src, ok := allowedSourceHosts[host]
	if !ok {
		return "", "", ErrSourceUnsupported
	}
	return src, rawURL, nil
}

// FromURL получает бандл по сети. github.com реализован полностью (GitHub
// REST contents API, публичные репозитории); clawhub.ai/skills.sh не имеют
// документированной в этом контракте формы API — решение (см.
// server2/docs/decisions.md, раздел T-028): вернуть ErrSourceUnsupported
// (502 upstream_unavailable у вызывающего кода) вместо угадывания формата.
func FromURL(ctx context.Context, rawURL string) (name, description, content string, files []FileInput, err error) {
	src, resolved, err := resolveSource(rawURL)
	if err != nil {
		return "", "", "", nil, err
	}
	switch src {
	case SourceGitHub:
		return fromGitHub(ctx, resolved)
	default:
		return "", "", "", nil, ErrSourceUnsupported
	}
}

// EnsureByNameOrImport — "переиспользовать уже существующий в воркспейсе
// навык с тем же именем, и только если такого нет — импортировать по
// source_url" (contract §10.6, createAgentFromTemplate). Возвращает id
// навыка, готового к привязке агенту.
func EnsureByNameOrImport(ctx context.Context, db *store.Store, workspaceID, actorID, name, sourceURL string) (string, error) {
	st := NewStore(db)
	if existing, ok, err := st.ByName(ctx, workspaceID, name); err != nil {
		return "", err
	} else if ok {
		return existing.ID, nil
	}
	fmName, fmDesc, content, files, err := FromURL(ctx, sourceURL)
	if err != nil {
		return "", err
	}
	if fmName == "" {
		fmName = name
	}
	sk, _, err := st.Create(ctx, CreateParams{
		WorkspaceID: workspaceID, Name: fmName, Summary: fmDesc, Content: content, CreatedBy: actorID, Files: files,
	})
	if err != nil {
		return "", err
	}
	return sk.ID, nil
}

// applyOnConflict реализует таблицу "Стратегия при конфликте имени" §2.
func applyOnConflict(ctx context.Context, db *store.Store, st *Store, workspaceID, actorID, onConflict string,
	name, description, content string, files []FileInput) Result {
	existing, hasExisting, err := st.ByName(ctx, workspaceID, name)
	if err != nil {
		return Result{Status: "failed", Reason: err.Error()}
	}
	if !hasExisting {
		sk, fs, err := st.Create(ctx, CreateParams{WorkspaceID: workspaceID, Name: name, Summary: description, Content: content, CreatedBy: actorID, Files: files})
		if err != nil {
			return Result{Status: "failed", Reason: err.Error()}
		}
		return Result{Status: "created", Skill: sk.WithFiles(fs)}
	}
	identity := &ExistingIdentity{ID: existing.ID, Name: existing.Name, CanOverwrite: existing.CreatedBy != nil && *existing.CreatedBy == actorID}
	if existing.CreatedBy != nil {
		identity.CreatedBy = *existing.CreatedBy
	}
	switch onConflict {
	case "skip":
		return Result{Status: "skipped", ExistingSkill: identity}
	case "overwrite":
		if !identity.CanOverwrite {
			return Result{Status: "failed", Reason: "only the skill's creator can overwrite it", ExistingSkill: identity}
		}
		if err := st.Update(ctx, workspaceID, existing.ID, UpdateParams{
			Summary: &description, Content: &content, ReplaceFiles: true, Files: files,
		}); err != nil {
			return Result{Status: "failed", Reason: err.Error()}
		}
		sk, _ := st.Get(ctx, workspaceID, existing.ID)
		fs, _ := st.ListFiles(ctx, existing.ID)
		return Result{Status: "updated", Skill: sk.WithFiles(fs)}
	case "rename":
		for i := 2; i <= 51; i++ {
			candidate := name + "-" + strconv.Itoa(i)
			if _, taken, _ := st.ByName(ctx, workspaceID, candidate); !taken {
				sk, fs, err := st.Create(ctx, CreateParams{WorkspaceID: workspaceID, Name: candidate, Summary: description, Content: content, CreatedBy: actorID, Files: files})
				if err != nil {
					return Result{Status: "failed", Reason: err.Error()}
				}
				return Result{Status: "created", Skill: sk.WithFiles(fs)}
			}
		}
		return Result{Status: "failed", Reason: "could not find a free name after 50 attempts", ExistingSkill: identity}
	default: // "fail"
		return Result{Status: "conflict", ExistingSkill: identity}
	}
}
