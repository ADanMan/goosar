package note

import (
	"testing"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

func TestParseMentions(t *testing.T) {
	content := "please look at this @agent:11111111-1111-1111-1111-111111111111 and " +
		"@squad:22222222-2222-2222-2222-222222222222, also @agent:11111111-1111-1111-1111-111111111111 again"
	got := parseMentions(content)
	if len(got) != 2 {
		t.Fatalf("expected 2 deduplicated mentions, got %d: %+v", len(got), got)
	}
	if got[0].kind != "agent" || got[0].id != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("unexpected first mention: %+v", got[0])
	}
	if got[1].kind != "squad" || got[1].id != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("unexpected second mention: %+v", got[1])
	}
	if len(parseMentions("no mentions here")) != 0 {
		t.Error("plain text should yield no mentions")
	}
}

func TestIsNoteComment(t *testing.T) {
	// Контракт: "комментарий, начинающийся с /note" — буквальная проверка
	// префикса (без требования словесной границы), поэтому "/notebook..."
	// тоже попадает под правило "никогда не запускает агентов".
	cases := map[string]bool{
		"/note this is a private note":            true,
		"  /NOTE leading spaces":                  true,
		"/notebook is a literal prefix match too": true,
		"not /note at the start":                  false,
		"regular comment":                         false,
		"":                                        false,
	}
	for in, want := range cases {
		if got := isNoteComment(in); got != want {
			t.Errorf("isNoteComment(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCanEditComment(t *testing.T) {
	author := Comment{AuthorType: "member", AuthorID: "u1"}
	if !canEditComment(author, "u1", httpapi.RoleMember) {
		t.Error("author should be able to edit their own comment")
	}
	if canEditComment(author, "u2", httpapi.RoleMember) {
		t.Error("a different member without owner/admin should not be able to edit")
	}
	if !canEditComment(author, "u2", httpapi.RoleAdmin) {
		t.Error("an admin should be able to edit someone else's comment")
	}
	if !canEditComment(author, "u2", httpapi.RoleOwner) {
		t.Error("an owner should be able to edit someone else's comment")
	}
}

func TestApplySummary(t *testing.T) {
	short := Comment{Content: "short comment"}
	applySummary(&short)
	if short.ContentTruncated != nil {
		t.Error("short content should not be marked truncated")
	}

	long := Comment{Content: repeatRune('x', 250)}
	applySummary(&long)
	if long.ContentTruncated == nil || !*long.ContentTruncated {
		t.Error("long content should be marked truncated")
	}
	if got := len([]rune(long.Content)); got != summaryRuneLimit {
		t.Errorf("truncated content length = %d, want %d", got, summaryRuneLimit)
	}
}

func repeatRune(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}

func TestFoldResolvedThreads(t *testing.T) {
	now := time.Now()
	rootUnresolved := Comment{ID: "r1"}
	rootResolved := Comment{ID: "r2", ResolvedAt: &now}
	reply := Comment{ID: "r2-reply", ParentID: strPtr("r2")}
	out := foldResolvedThreads([]Comment{rootUnresolved, rootResolved, reply})

	var foundUnresolved, foundResolvedRoot bool
	for _, c := range out {
		if c.ID == "r1" {
			foundUnresolved = true
		}
		if c.ID == "r2" {
			foundResolvedRoot = true
			if c.ThreadResolved == nil || !*c.ThreadResolved {
				t.Error("resolved root should have ThreadResolved=true")
			}
			if c.FoldedCount == nil || *c.FoldedCount != 1 {
				t.Errorf("expected FoldedCount=1, got %v", c.FoldedCount)
			}
		}
		if c.ID == "r2-reply" {
			t.Error("reply of a resolved thread should be folded away")
		}
	}
	if !foundUnresolved || !foundResolvedRoot {
		t.Error("both roots should be present in the folded output")
	}
}

func strPtr(s string) *string { return &s }
