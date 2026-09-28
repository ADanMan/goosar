package task

import "testing"

// ---------------------------------------------------------------------------
// Нумерация/поиск по identifier (contract §1.1/§1.4: "ENG-42" -> number=42)
// ---------------------------------------------------------------------------

func TestParseIdentifierNumber(t *testing.T) {
	cases := []struct {
		prefix, q string
		wantN     int64
		wantOK    bool
	}{
		{"ENG", "ENG-42", 42, true},
		{"ENG", "eng-42", 42, true}, // регистронезависимо
		{"ENG", "ENG-0", 0, true},
		{"ENG", "ENG-", 0, false},
		{"ENG", "ENG42", 0, false}, // без дефиса — не идентификатор
		{"ENG", "some text", 0, false},
		{"ENG", "OTHER-42", 0, false}, // чужой префикс
		{"ENG", "", 0, false},
	}
	for _, c := range cases {
		n, ok := parseIdentifierNumber(c.prefix, c.q)
		if ok != c.wantOK || (ok && n != c.wantN) {
			t.Errorf("parseIdentifierNumber(%q, %q) = (%d, %v), want (%d, %v)", c.prefix, c.q, n, ok, c.wantN, c.wantOK)
		}
	}
}

// ---------------------------------------------------------------------------
// Разбор списочных фильтров (contract §1.4)
// ---------------------------------------------------------------------------

func TestParseTypeIDList(t *testing.T) {
	got := ParseTypeIDList("agent:11111111-1111-1111-1111-111111111111,squad:22222222-2222-2222-2222-222222222222")
	want := []TypeID{
		{Type: "agent", ID: "11111111-1111-1111-1111-111111111111"},
		{Type: "squad", ID: "22222222-2222-2222-2222-222222222222"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseTypeIDList: got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ParseTypeIDList[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// элементы без ":" молча пропускаются, не паникуют и не превращаются в мусор.
	if got := ParseTypeIDList("garbage,agent:x"); len(got) != 1 || got[0] != (TypeID{Type: "agent", ID: "x"}) {
		t.Errorf("ParseTypeIDList должен пропускать элементы без ':': got %+v", got)
	}
	if got := ParseTypeIDList(""); got != nil {
		t.Errorf("ParseTypeIDList(\"\") должен вернуть nil, got %+v", got)
	}
}

func TestSplitCSV(t *testing.T) {
	if got := splitCSV("a, b ,,c"); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("splitCSV должен обрезать пробелы и пропускать пустые элементы, got %+v", got)
	}
	if got := splitCSV(""); got != nil {
		t.Errorf("splitCSV(\"\") должен вернуть nil, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Правило автозапуска (contract §1.9) — чистая часть без БД
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

func TestAutostartCases_CreateWithAssigneeNotBacklog(t *testing.T) {
	after := Issue{Status: "todo", AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
	caseAssignment, caseStatus := autostartCases(Issue{}, after, true)
	if !caseAssignment {
		t.Error("создание задачи сразу с исполнителем не в backlog должно ставить в очередь (случай 1)")
	}
	if caseStatus {
		t.Error("случай 2 (переоткрытие) не должен срабатывать при создании")
	}
}

func TestAutostartCases_CreateInBacklogDoesNotTrigger(t *testing.T) {
	after := Issue{Status: "backlog", AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
	caseAssignment, caseStatus := autostartCases(Issue{}, after, true)
	if caseAssignment || caseStatus {
		t.Error("создание задачи в backlog не должно ставить агента в очередь, даже если назначен исполнитель")
	}
}

func TestAutostartCases_AssigneeChangedOnExistingNonBacklogIssue(t *testing.T) {
	before := Issue{Status: "todo", AssigneeType: strPtr("agent"), AssigneeID: strPtr("old")}
	after := Issue{Status: "todo", AssigneeType: strPtr("agent"), AssigneeID: strPtr("new")}
	caseAssignment, caseStatus := autostartCases(before, after, false)
	if !caseAssignment {
		t.Error("смена исполнителя на непустом статусе должна ставить в очередь (случай 1)")
	}
	if caseStatus {
		t.Error("случай 2 не должен срабатывать здесь — статус не менялся с backlog")
	}
}

func TestAutostartCases_UnchangedAssigneeDoesNotTrigger(t *testing.T) {
	before := Issue{Status: "todo", AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
	after := Issue{Status: "in_progress", AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
	caseAssignment, caseStatus := autostartCases(before, after, false)
	if caseAssignment {
		t.Error("исполнитель не менялся — случай 1 не должен срабатывать просто от смены статуса todo->in_progress")
	}
	if caseStatus {
		t.Error("случай 2 требует именно перехода ИЗ backlog, не из todo")
	}
}

func TestAutostartCases_StatusReopenedFromBacklogWithExistingAgent(t *testing.T) {
	before := Issue{Status: "backlog", AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
	after := Issue{Status: "todo", AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
	caseAssignment, caseStatus := autostartCases(before, after, false)
	if caseAssignment {
		t.Error("исполнитель не менялся при переходе из backlog — не случай 1")
	}
	if !caseStatus {
		t.Error("переход backlog -> todo с уже существующим исполнителем-агентом должен ставить в очередь (случай 2)")
	}
}

func TestAutostartCases_StatusReopenedIntoTerminalDoesNotTrigger(t *testing.T) {
	for _, terminal := range []string{"done", "cancelled"} {
		before := Issue{Status: "backlog", AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
		after := Issue{Status: terminal, AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
		_, caseStatus := autostartCases(before, after, false)
		if caseStatus {
			t.Errorf("переход backlog -> %s (терминальный) не должен ставить агента в очередь", terminal)
		}
	}
}

func TestAutostartCases_ReopenWithoutPriorAssigneeDoesNotTrigger(t *testing.T) {
	before := Issue{Status: "backlog"} // не было исполнителя вовсе
	after := Issue{Status: "todo", AssigneeType: strPtr("agent"), AssigneeID: strPtr("a1")}
	caseAssignment, caseStatus := autostartCases(before, after, false)
	// это на самом деле случай 1 (assignee_changed: до — nil, после — agent/a1), не случай 2.
	if !caseAssignment {
		t.Error("назначение исполнителя одновременно с переходом из backlog — это случай 1 (assignee_changed), должен сработать")
	}
	if caseStatus {
		t.Error("случай 2 требует, чтобы исполнитель УЖЕ БЫЛ до перехода — здесь его не было")
	}
}
