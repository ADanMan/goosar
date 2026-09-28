package skill

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidPath — путь абсолютный или выходит за пределы каталога навыка
// после нормализации (contract "Валидация путей файлов").
var ErrInvalidPath = errors.New("skill: некорректный путь файла")

// ErrReservedPath — попытка создать/заменить SKILL.md через файловый API.
var ErrReservedPath = errors.New("skill: SKILL.md — зарезервированный путь")

// ErrFileNotFound — файл не существует либо принадлежит другому навыку.
var ErrFileNotFound = errors.New("skill: файл не найден")

// ValidatePath — правило контракта: путь отклоняется, если абсолютный, или
// после normalize (path.Clean) начинается с "..".
func ValidatePath(p string) error {
	if p == "" {
		return ErrInvalidPath
	}
	if strings.HasPrefix(p, "/") {
		return ErrInvalidPath
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ErrInvalidPath
	}
	return nil
}

const fileColumns = `id, capability_id, capf_path, capf_body, created_at, updated_at`

func scanFile(row pgx.Row) (File, error) {
	var f File
	if err := row.Scan(&f.ID, &f.SkillID, &f.Path, &f.Content, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return File{}, err
	}
	return f, nil
}

// ListFiles — вспомогательные файлы навыка.
func (s *Store) ListFiles(ctx context.Context, skillID string) ([]File, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+fileColumns+` FROM capability_files WHERE capability_id = $1 ORDER BY capf_path ASC`, skillID)
	if err != nil {
		return nil, fmt.Errorf("skill: файлы навыка: %w", err)
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpsertFile — создать или заменить один файл (upsertSkillFile). Путь
// SKILL.md зарезервирован (ErrReservedPath), путь должен пройти ValidatePath.
func (s *Store) UpsertFile(ctx context.Context, skillID, path, content string) (File, error) {
	if path == ReservedFilePath {
		return File{}, ErrReservedPath
	}
	if err := ValidatePath(path); err != nil {
		return File{}, err
	}
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO capability_files (capability_id, capf_path, capf_body)
		VALUES ($1,$2,$3)
		ON CONFLICT (capability_id, capf_path) DO UPDATE SET capf_body = EXCLUDED.capf_body, updated_at = now()
		RETURNING `+fileColumns, skillID, path, content)
	return scanFile(row)
}

// DeleteFile — удалить один файл; ErrFileNotFound, если файл не принадлежит
// этому навыку.
func (s *Store) DeleteFile(ctx context.Context, skillID, fileID string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM capability_files WHERE id = $1 AND capability_id = $2`, fileID, skillID)
	if err != nil {
		return fmt.Errorf("skill: удаление файла: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrFileNotFound
	}
	return nil
}
