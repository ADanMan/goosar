package tagging

import "testing"

func TestValidLabelName(t *testing.T) {
	if !validLabelName("bug") {
		t.Error("bug должен быть валиден")
	}
	if validLabelName("") {
		t.Error("пустое имя не должно быть валидно")
	}
	if validLabelName(string(make([]byte, 33))) {
		t.Error("имя длиннее 32 байт не должно быть валидно")
	}
	if validLabelName("x\x00y") {
		t.Error("имя с управляющим символом не должно быть валидно")
	}
}

func TestNormalizeColor(t *testing.T) {
	cases := map[string]string{
		"336699":    "#336699",
		"#336699":   "#336699",
		"ABCDEF":    "#abcdef",
		" #fff000 ": "#fff000",
	}
	for in, want := range cases {
		if got := normalizeColor(in); got != want {
			t.Errorf("normalizeColor(%q) = %q, want %q", in, got, want)
		}
	}
	if !hexColorRe.MatchString(normalizeColor("336699")) {
		t.Error("normalizeColor результат должен проходить hexColorRe")
	}
	if hexColorRe.MatchString(normalizeColor("not-a-color")) {
		t.Error("невалидный цвет не должен проходить hexColorRe")
	}
}

func TestValidPropertyConfig(t *testing.T) {
	if err := validPropertyConfig("text", PropertyConfig{}); err != nil {
		t.Errorf("text без options должен быть валиден: %v", err)
	}
	if err := validPropertyConfig("select", PropertyConfig{}); err == nil {
		t.Error("select без options должен быть невалиден")
	}
	cfg := PropertyConfig{Options: []PropertyOption{{Name: "Low", Color: "#fff"}, {Name: "High", Color: "#000"}}}
	if err := validPropertyConfig("select", cfg); err != nil {
		t.Errorf("select с валидными options: %v", err)
	}
	if cfg.Options[0].ID == "" || cfg.Options[1].ID == "" {
		t.Error("отсутствующие id вариантов должны быть сгенерированы")
	}
	if cfg.Options[0].ID == cfg.Options[1].ID {
		t.Error("сгенерированные id вариантов не должны совпадать")
	}
	dup := PropertyConfig{Options: []PropertyOption{{Name: "Low", Color: "#fff"}, {Name: "Low", Color: "#000"}}}
	if err := validPropertyConfig("multi_select", dup); err == nil {
		t.Error("дублирующиеся имена вариантов должны быть отклонены")
	}
}

func TestValidatePropertyValue(t *testing.T) {
	textProp := Property{Type: "text"}
	if _, err := validatePropertyValue(textProp, []byte(`""`)); err == nil {
		t.Error("пустая строка для text должна быть отклонена")
	}
	if _, err := validatePropertyValue(textProp, []byte(`"hello"`)); err != nil {
		t.Errorf("непустая строка для text должна проходить: %v", err)
	}

	numProp := Property{Type: "number"}
	if _, err := validatePropertyValue(numProp, []byte(`"nope"`)); err == nil {
		t.Error("строка для number должна быть отклонена")
	}
	if _, err := validatePropertyValue(numProp, []byte(`42`)); err != nil {
		t.Errorf("число для number должно проходить: %v", err)
	}

	dateProp := Property{Type: "date"}
	if _, err := validatePropertyValue(dateProp, []byte(`"2026-13-40"`)); err == nil {
		t.Error("невалидная дата должна быть отклонена")
	}
	if _, err := validatePropertyValue(dateProp, []byte(`"2026-01-15"`)); err != nil {
		t.Errorf("валидная дата должна проходить: %v", err)
	}

	selectProp := Property{Type: "select", Config: PropertyConfig{Options: []PropertyOption{{ID: "opt-1", Name: "A"}}}}
	if _, err := validatePropertyValue(selectProp, []byte(`"opt-missing"`)); err == nil {
		t.Error("id варианта, которого нет в config, должен быть отклонён")
	}
	if _, err := validatePropertyValue(selectProp, []byte(`"opt-1"`)); err != nil {
		t.Errorf("существующий id варианта должен проходить: %v", err)
	}

	multiProp := Property{Type: "multi_select", Config: PropertyConfig{Options: []PropertyOption{
		{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"},
	}}}
	out, err := validatePropertyValue(multiProp, []byte(`["c","a","c","a"]`))
	if err != nil {
		t.Fatalf("multi_select с дублями должен проходить: %v", err)
	}
	if string(out) != `["a","c"]` {
		t.Errorf("normalizeMultiSelect должен схлопнуть дубли и упорядочить по config: got %s", out)
	}
}

func TestReservedPropertyNames(t *testing.T) {
	for _, name := range []string{"status", "priority", "labels", "metadata"} {
		if !reservedPropertyNames[name] {
			t.Errorf("%q должен быть зарезервирован", name)
		}
	}
	if reservedPropertyNames["my-custom-field"] {
		t.Error("обычное имя не должно быть зарезервировано")
	}
}
