package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/adanman/goosar/server2/internal/store"
)

// BundleFile — один вспомогательный файл внутри Bundle.
type BundleFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Bundle — навык в форме, готовой передать демону/CLI для локального
// исполнения (T-028, задел для соседнего домена daemon): SKILL.md + файлы,
// плюс content-хэш и суммарный размер для клиентского кэширования/сверки
// изменений без повторной передачи неизменённого бандла. Экспортировано по
// прямому пункту задачи этой сессии ("дай функцию соседу A:
// skill.ResolveBundles"); см. server2/docs/decisions.md, раздел T-028.
type Bundle struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Content     string       `json:"content"`
	Files       []BundleFile `json:"files"`
	Hash        string       `json:"hash"` // sha256 hex поверх стабильной сериализации ниже
	Size        int64        `json:"size"` // суммарные байты content + files (UTF-8)
}

// hashOf — детерминированная сериализация: имя + content + файлы,
// отсортированные по пути (порядок из БД уже ORDER BY capf_path, здесь
// пересортировано явно, чтобы не зависеть от вызывающего запроса).
func hashOf(name, content string, files []BundleFile) string {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write([]byte(content))
	for _, f := range files {
		h.Write([]byte{0})
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		h.Write([]byte(f.Content))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func toBundle(sk Skill, files []File) Bundle {
	bf := make([]BundleFile, len(files))
	size := int64(len(sk.Content))
	for i, f := range files {
		bf[i] = BundleFile{Path: f.Path, Content: f.Content}
		size += int64(len(f.Content))
	}
	sort.Slice(bf, func(i, j int) bool { return bf[i].Path < bf[j].Path })
	return Bundle{
		ID: sk.ID, Name: sk.Name, Description: sk.Description, Content: sk.Content,
		Files: bf, Hash: hashOf(sk.Name, sk.Content, bf), Size: size,
	}
}

// ResolveBundles возвращает Bundle для каждый из skillIDs, который
// существует и принадлежит workspaceID (id, не принадлежащие воркспейсу,
// молча пропускаются — вызывающий домен уже отфильтровал список по своим
// собственным правилам enabled/agent-привязки, отсюда id могут быть только
// заведомо валидными; на случай гонки — тихий пропуск лучше 500).
// Вызывающий (internal daemon) сам решает, какие skillIDs передавать —
// например список из operative_capabilities агента, где opcap_enabled=true
// (agent package не импортируется здесь намеренно, чтобы не создавать
// цикл skill<->agent; agent уже импортирует skill).
func ResolveBundles(ctx context.Context, db *store.Store, workspaceID string, skillIDs []string) ([]Bundle, error) {
	if len(skillIDs) == 0 {
		return []Bundle{}, nil
	}
	st := NewStore(db)
	out := make([]Bundle, 0, len(skillIDs))
	for _, id := range skillIDs {
		sk, err := st.Get(ctx, workspaceID, id)
		if err != nil {
			if err == ErrNotFound {
				continue
			}
			return nil, fmt.Errorf("skill: сборка бандла %s: %w", id, err)
		}
		files, err := st.ListFiles(ctx, sk.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, toBundle(sk, files))
	}
	return out, nil
}
