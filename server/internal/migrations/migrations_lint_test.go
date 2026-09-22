// Проверяет, что у каждой миграции есть парные up и down файлы.
package migrations

import "testing"

func TestMigrationFilesHaveMatchingDirections(t *testing.T) {
	ups, downs := map[string]bool{}, map[string]bool{}
	for dir, set := range map[string]map[string]bool{"up": ups, "down": downs} {
		files, err := Files(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			set[ExtractVersion(f)] = true
		}
	}
	for stem := range ups {
		if !downs[stem] {
			t.Errorf("migration %s has no .down.sql", stem)
		}
	}
	for stem := range downs {
		if !ups[stem] {
			t.Errorf("migration %s has no .up.sql", stem)
		}
	}
}
