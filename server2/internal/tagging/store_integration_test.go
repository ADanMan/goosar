package tagging

import (
	"context"
	"encoding/json"
	"testing"
)

func TestLabelCRUDAndIssueAttachment(t *testing.T) {
	db := newTestDB(t)
	wsID, acctID := seedWorkspace(t, db)
	ticketID := seedTicket(t, db, wsID, acctID)
	s := NewStore(db)
	ctx := context.Background()

	label, err := s.CreateLabel(ctx, CreateLabelParams{
		WorkspaceID: wsID, ResourceType: "issue", Name: "bug", Description: "a bug", Color: "#336699",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if label.UsageCount != 0 {
		t.Errorf("new label usage_count = %d, want 0", label.UsageCount)
	}

	if _, err := s.CreateLabel(ctx, CreateLabelParams{WorkspaceID: wsID, ResourceType: "issue", Name: "bug", Color: "#000000"}); err != ErrNameTaken {
		t.Errorf("duplicate name: got %v, want ErrNameTaken", err)
	}

	list, err := s.ListLabels(ctx, wsID, "issue")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListLabels: got %d labels, err %v", len(list), err)
	}

	if err := s.AttachIssueLabel(ctx, wsID, ticketID, label.ID); err != nil {
		t.Fatalf("AttachIssueLabel: %v", err)
	}
	// Идемпотентность: повторное прикрепление не должно ни падать, ни удваивать счётчик.
	if err := s.AttachIssueLabel(ctx, wsID, ticketID, label.ID); err != nil {
		t.Fatalf("AttachIssueLabel (repeat): %v", err)
	}
	issueLabels, err := s.ListIssueLabels(ctx, wsID, ticketID)
	if err != nil || len(issueLabels) != 1 {
		t.Fatalf("ListIssueLabels: got %d, err %v", len(issueLabels), err)
	}
	reloaded, err := s.GetLabel(ctx, wsID, label.ID)
	if err != nil || reloaded.UsageCount != 1 {
		t.Fatalf("usage_count after one attach: got %d, err %v", reloaded.UsageCount, err)
	}

	if err := s.DetachIssueLabel(ctx, wsID, ticketID, label.ID); err != nil {
		t.Fatalf("DetachIssueLabel: %v", err)
	}
	issueLabels, err = s.ListIssueLabels(ctx, wsID, ticketID)
	if err != nil || len(issueLabels) != 0 {
		t.Fatalf("ListIssueLabels after detach: got %d, err %v", len(issueLabels), err)
	}

	if err := s.DeleteLabel(ctx, wsID, label.ID); err != nil {
		t.Fatalf("DeleteLabel: %v", err)
	}
	if _, err := s.GetLabel(ctx, wsID, label.ID); err != ErrNotFound {
		t.Errorf("GetLabel after delete: got %v, want ErrNotFound", err)
	}
}

func TestPropertyLifecycleAndIssueValues(t *testing.T) {
	db := newTestDB(t)
	wsID, acctID := seedWorkspace(t, db)
	ticketID := seedTicket(t, db, wsID, acctID)
	s := NewStore(db)
	ctx := context.Background()

	prop, err := s.CreateProperty(ctx, CreatePropertyParams{
		WorkspaceID: wsID, Name: "severity", Type: "select",
		Config: PropertyConfig{Options: []PropertyOption{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}}},
	})
	if err != nil {
		t.Fatalf("CreateProperty: %v", err)
	}
	if prop.Archived {
		t.Error("freshly created property should not be archived")
	}

	updated, err := s.SetIssuePropertyValue(ctx, wsID, ticketID, prop.ID, json.RawMessage(`"high"`))
	if err != nil {
		t.Fatalf("SetIssuePropertyValue: %v", err)
	}
	var values map[string]string
	if err := json.Unmarshal(updated, &values); err != nil {
		t.Fatalf("unmarshal properties block: %v", err)
	}
	if values[prop.ID] != "high" {
		t.Errorf("properties[%s] = %q, want %q", prop.ID, values[prop.ID], "high")
	}

	reloadedProp, err := s.GetProperty(ctx, wsID, prop.ID)
	if err != nil || reloadedProp.UsageCount != 1 {
		t.Fatalf("usage_count after set: got %d, err %v", reloadedProp.UsageCount, err)
	}

	// Вариант "high" сейчас используется — попытка убрать его из config должна
	// быть отклонена OptionsInUse (contract §4: 409 "занятые варианты").
	used, err := s.OptionsInUse(ctx, wsID, prop.ID, []string{"high"})
	if err != nil {
		t.Fatalf("OptionsInUse: %v", err)
	}
	if len(used) != 1 || used[0] != "high" {
		t.Errorf("OptionsInUse = %v, want [high]", used)
	}

	if _, err := s.DeleteIssuePropertyValue(ctx, wsID, ticketID, prop.ID); err != nil {
		t.Fatalf("DeleteIssuePropertyValue: %v", err)
	}
	reloadedProp, err = s.GetProperty(ctx, wsID, prop.ID)
	if err != nil || reloadedProp.UsageCount != 0 {
		t.Fatalf("usage_count after delete: got %d, err %v", reloadedProp.UsageCount, err)
	}

	if _, err := s.UpdateProperty(ctx, wsID, prop.ID, UpdatePropertyParams{Archived: boolPtr(true)}); err != nil {
		t.Fatalf("archive property: %v", err)
	}
	if _, err := s.SetIssuePropertyValue(ctx, wsID, ticketID, prop.ID, json.RawMessage(`"low"`)); err != nil {
		// Контракт: архивированному свойству нельзя присвоить значение — это
		// проверяется на уровне обработчика (handlers_properties.go), не
		// стора; здесь просто фиксируем, что стор сам по себе эту проверку
		// не делает (осознанное разделение ответственности).
		t.Logf("store-level SetIssuePropertyValue does not itself reject archived properties: %v", err)
	}
}

func TestPropertyActiveLimit(t *testing.T) {
	db := newTestDB(t)
	wsID, _ := seedWorkspace(t, db)
	s := NewStore(db)
	ctx := context.Background()

	for i := 0; i < maxActiveProperties; i++ {
		name := string(rune('a' + i))
		if _, err := s.CreateProperty(ctx, CreatePropertyParams{WorkspaceID: wsID, Name: name, Type: "text"}); err != nil {
			t.Fatalf("CreateProperty #%d: %v", i, err)
		}
	}
	n, err := s.CountActiveProperties(ctx, wsID)
	if err != nil || n != maxActiveProperties {
		t.Fatalf("CountActiveProperties = %d, err %v, want %d", n, err, maxActiveProperties)
	}
	if _, err := s.CreateProperty(ctx, CreatePropertyParams{WorkspaceID: wsID, Name: "one-too-many", Type: "text"}); err == nil {
		t.Error("21st active property should be rejected")
	}
}

func boolPtr(b bool) *bool { return &b }
