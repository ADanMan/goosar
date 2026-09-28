package importer

import "testing"

func TestReport_addAccumulates(t *testing.T) {
	r := newReport(WorkspaceRef{SourceID: "ws1", Slug: "acme"}, false)
	r.add("tickets", 3)
	r.add("tickets", 2)
	if r.Counts["tickets"] != 5 {
		t.Errorf("Counts[tickets] = %d, want 5", r.Counts["tickets"])
	}
}

func TestReport_skipRecordsCategory(t *testing.T) {
	r := newReport(WorkspaceRef{SourceID: "ws1"}, false)
	r.skip("assets_attachments", 7, "нет доступа к объектному хранилищу")
	if len(r.Skipped) != 1 {
		t.Fatalf("len(Skipped) = %d, want 1", len(r.Skipped))
	}
	if r.Skipped[0].Category != "assets_attachments" || r.Skipped[0].Count != 7 {
		t.Errorf("Skipped[0] = %+v, unexpected", r.Skipped[0])
	}
}

func TestReport_sortedCategoriesIsDeterministic(t *testing.T) {
	r := newReport(WorkspaceRef{}, false)
	r.add("zeta", 1)
	r.add("alpha", 1)
	r.add("mid", 1)
	got := r.sortedCategories()
	want := []string{"alpha", "mid", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sortedCategories()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
