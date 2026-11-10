package layout

import (
	"encoding/json"
	"strings"
	"testing"
)

// Инструкция по ДОУ важнее ГОСТ, ГОСТ закрывает то, о чем она молчит, шаблон - остальное
func TestLoad_FormatPrefersInstruction(t *testing.T) {
	format := test_Format(t)
	instruction := []bool{
		format.MarginLeftMM == 30, format.MarginRightMM == 20, format.MarginTopMM == 25, format.MarginBottomMM == 15,
		format.FontSizePt == 13, format.TextLineSpacing == 1.15,
		format.PageNumberPart == PageNumberFooter, format.PageNumberAlign == "right", !format.PageNumberOnFirstPage,
		format.InitialsSeparator == " ", format.HeaderNameOrder == NameOrderInitialsFirst,
		format.CommitteeMemberLabel == CommitteeLabelEach, format.ExecutorNameOrder == NameOrderInitialsFirst,
	}
	gost := []bool{
		format.PageWidthMM == 210, format.FirstLineIndentMM == 12.5, format.TextAlign == "both",
		format.RequisiteLineSpacing == 1, format.AddresseeZoneMM == 80, format.HeaderFooterDistanceMM == 10,
	}
	base := []bool{format.EmblemHeightMM == 18, format.SmallFontSizePt == 12}
	for group, checks := range map[string][]bool{"instruction": instruction, "gost": gost, "base": base} {
		for index, ok := range checks {
			if !ok {
				t.Fatalf("%s rule %d: %+v", group, index, format)
			}
		}
	}
}

// LAYOUT_FORMAT_PRIORITY=gost: ГОСТ первым, Инструкция только там, где ГОСТ молчит
func TestLoad_FormatOrderFromSettings(t *testing.T) {
	format := test_Format(t, " gost ", "")
	if format.MarginLeftMM != 20 || format.MarginRightMM != 10 || format.FontSizePt != 14 || format.PageNumberPart != PageNumberHeader || format.InitialsSeparator != "" {
		t.Fatalf("gost first %+v", format)
	}
	if _, err := Load_Format([]string{"unknown"}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown profile %v", err)
	}
}

// Каждое правило профиля известно, указывает пункт документа, и профили вместе задают все правила
func TestFormatProfilesAreComplete(t *testing.T) {
	profiles, err := read_FormatProfiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"instruction", "gost", "base"} {
		if _, found := profiles[name]; !found {
			t.Fatalf("no profile %s", name)
		}
	}
	known := map[string]bool{}
	for _, key := range find_MissingRules(map[string]json.RawMessage{}) {
		known[key] = true
	}
	for name, profile := range profiles {
		if strings.TrimSpace(profile.Title) == "" {
			t.Fatalf("profile %s has no title", name)
		}
		for key, rule := range profile.Rules {
			if !known[key] || strings.TrimSpace(rule.Ref) == "" {
				t.Fatalf("profile %s rule %s: known %v, ref %q", name, key, known[key], rule.Ref)
			}
		}
	}
	if missing := find_MissingRules(map[string]json.RawMessage{"pageWidthMM": nil}); len(missing) != len(known)-1 {
		t.Fatalf("missing %v", missing)
	}
}

func TestFormatDateAndExecutor(t *testing.T) {
	format, gost := test_Format(t), test_Format(t, "gost")
	if date := format.format_Date(testNow); date != "15.08.2026" {
		t.Fatalf("date %q", date)
	}
	for text, want := range map[string]string{
		"Петрова Мария Сергеевна, +7 (495) 123-45-67": "М. С. Петрова, +7 (495) 123-45-67",
		"Петрова М.С.":                        "М. С. Петрова",
		"отдел кадров, +7 (495) 123-45-67":    "отдел кадров, +7 (495) 123-45-67",
		"инженер Петрова Мария Сергеевна, 12": "инженер Петрова Мария Сергеевна, 12",
	} {
		if got := format.executor_Text(text); got != want {
			t.Fatalf("%q -> %q", text, got)
		}
	}
	if got := gost.executor_Text("Петрова Мария Сергеевна, +7 (495) 123-45-67"); got != "Петрова Мария Сергеевна, +7 (495) 123-45-67" {
		t.Fatalf("gost executor as written %q", got)
	}
}

// Образцы ФИО в подсказке модели записаны по тем же правилам, что документ
func TestComposePromptFollowsFormat(t *testing.T) {
	prompt := build_ComposePrompt(test_Format(t))
	if strings.Contains(prompt, "{sample") || !strings.Contains(prompt, "П. П. Сидорову") || !strings.Contains(prompt, "М. С. Петровой") {
		t.Fatalf("instruction prompt %s", prompt)
	}
	if gostPrompt := build_ComposePrompt(test_Format(t, "gost")); !strings.Contains(gostPrompt, "Сидорову П.П.") {
		t.Fatalf("gost prompt %s", gostPrompt)
	}
}
