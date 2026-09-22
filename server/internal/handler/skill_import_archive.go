package handler

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	skillpkg "github.com/adanman/goosar/server/internal/skill"
)

const maxImportArchiveUploadSize = 16 << 20

func isMultipartForm(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data")
}

func (h *Handler) importSkillFromArchive(w http.ResponseWriter, r *http.Request, workspaceID string, workspaceUUID, creatorUUID pgtype.UUID, creatorID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportArchiveUploadSize)
	if err := r.ParseMultipartForm(maxImportArchiveUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload or file exceeds the size limit")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	onConflict := r.FormValue("on_conflict")
	if !validImportOnConflict(onConflict) {
		writeError(w, http.StatusBadRequest, "on_conflict must be one of: fail, overwrite, rename, skip")
		return
	}
	strategy := onConflict
	if strategy == "" {
		strategy = importOnConflictFail
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `a skill archive file is required (form field "file")`)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read uploaded file")
		return
	}

	filename := ""
	if header != nil {
		filename = header.Filename
	}
	imported, err := parseSkillArchive(data, filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.finishSkillImport(w, r, workspaceID, workspaceUUID, creatorUUID, creatorID, strategy, true, imported)
}

func parseSkillArchive(data []byte, filename string) (*importedSkill, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("uploaded file is not a valid .skill/.zip archive")
	}

	var skillMd *zip.File
	rootPrefix := ""
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(f.Name)
		if !strings.EqualFold(path.Base(clean), skillpkg.ContentFilename) {
			continue
		}
		if !validateFilePath(clean) {
			continue
		}
		prefix := archiveEntryPrefix(clean)
		if skillMd == nil || len(prefix) < len(rootPrefix) {
			skillMd = f
			rootPrefix = prefix
		}
	}
	if skillMd == nil {
		return nil, fmt.Errorf("archive does not contain a SKILL.md")
	}

	content, err := readZipFile(skillMd, maxImportFileSize)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}

	name, description := skillpkg.ParseSkillFrontmatter(content)
	if name == "" {
		name = skillNameFromArchive(rootPrefix, filename)
	}
	if name == "" {
		return nil, fmt.Errorf("could not determine the skill name: SKILL.md has no name field and the archive is unnamed")
	}

	imported := &importedSkill{
		name:        name,
		description: description,
		content:     content,
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(f.Name)

		if rootPrefix != "" && !strings.HasPrefix(clean, rootPrefix) {
			continue
		}
		rel := strings.TrimPrefix(clean, rootPrefix)
		if rel == "" {
			continue
		}

		if strings.EqualFold(path.Base(rel), skillpkg.ContentFilename) {
			continue
		}
		if isIgnoredArchiveEntry(rel) {
			continue
		}

		if !validateFilePath(rel) {
			continue
		}
		fileContent, ferr := readZipFile(f, maxImportFileSize)
		if ferr != nil {

			continue
		}

		if err := imported.addFile(rel, fileContent); err != nil {
			return nil, err
		}
	}

	sort.Slice(imported.files, func(i, j int) bool {
		return imported.files[i].path < imported.files[j].path
	})
	return imported, nil
}

func archiveEntryPrefix(cleanName string) string {
	dir := path.Dir(cleanName)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir + "/"
}

func skillNameFromArchive(rootPrefix, filename string) string {
	if rootPrefix != "" {
		base := path.Base(strings.TrimSuffix(rootPrefix, "/"))
		if base != "." && base != "/" && base != ".." {
			return base
		}
	}
	clean := strings.ReplaceAll(filename, "\\", "/")
	base := path.Base(clean)
	if ext := path.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

func isIgnoredArchiveEntry(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "__MACOSX" || strings.HasPrefix(seg, ".") {
			return true
		}
	}
	switch strings.ToLower(path.Base(rel)) {
	case "license", "license.md", "license.txt":
		return true
	}
	return false
}

func readZipFile(f *zip.File, maxSize int64) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(io.LimitReader(rc, maxSize+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxSize {
		return "", fmt.Errorf("file %q exceeds %d bytes", f.Name, maxSize)
	}
	return string(data), nil
}
