package importer

import "sort"

// Report — сводка одного запуска импорта, печатается cmd/import в stdout как
// JSON (см. docs/51-data-model.md, конец «Перенос данных»: «Отчёт импортёра
// явно перечисляет каждую из категорий [не перенесённого] с количеством
// пропущенных объектов»).
type Report struct {
	Workspace      WorkspaceRef      `json:"workspace"`
	DryRun         bool              `json:"dry_run"`
	MigrationsRun  []int             `json:"migrations_applied,omitempty"`
	Counts         map[string]int    `json:"transferred"`
	Skipped        []SkippedCategory `json:"skipped_by_design"`
}

// WorkspaceRef идентифицирует перенесённый воркспейс в отчёте.
type WorkspaceRef struct {
	SourceID string `json:"source_id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
}

// SkippedCategory — одна категория данных, которую импортёр сознательно не
// переносит (см. docs/51-data-model.md, «Что не переносится через API, и
// почему», плюс собственные пробелы спецификации из decisions.md).
type SkippedCategory struct {
	Category string `json:"category"`
	Count    int    `json:"count,omitempty"`
	Reason   string `json:"reason"`
}

func newReport(ws WorkspaceRef, dryRun bool) *Report {
	return &Report{
		Workspace: ws,
		DryRun:    dryRun,
		Counts:    map[string]int{},
	}
}

func (r *Report) add(category string, n int) {
	r.Counts[category] += n
}

func (r *Report) skip(category string, count int, reason string) {
	r.Skipped = append(r.Skipped, SkippedCategory{Category: category, Count: count, Reason: reason})
}

// sortedCounts — вспомогательная функция для тестов/логов с предсказуемым
// порядком ключей.
func (r *Report) sortedCategories() []string {
	keys := make([]string, 0, len(r.Counts))
	for k := range r.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
