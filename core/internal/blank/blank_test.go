package blank

import (
	"strings"
	"testing"
)

func TestParseAndAdvance_Blank(t *testing.T) {
	plain := strings.Join([]string{
		"О создании тестовой группы",
		"В целях ______ [1]",
		"5. [Лишний пункт ______.] [2]",
		"И. О. Фамилия [3]",
		"Примечания по заполнению",
		"1. Цель. Укажите цель работы.",
		"2. Дополнение. Пункт необязательный. Иначе удалите пункт.",
		"3. Подписант. Укажите фамилию.",
	}, "\n")
	item, err := Parse_BlankText("test.docx", plain)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "О создании тестовой группы" || len(item.Fields) != 3 {
		t.Fatalf("parsed %+v", item)
	}
	if item.Fields[0].Label != "Цель" || item.Fields[0].Question != "Укажите цель работы." || item.Fields[0].Optional {
		t.Fatalf("first field %+v", item.Fields[0])
	}
	if !item.Fields[1].Optional {
		t.Fatal("optional field was required")
	}
	other, err := Parse_BlankText("other.docx", "О создании проектного комитета\nТекст ______ [1]\nПримечания\n1. Проект. Укажите проект.")
	if err != nil {
		t.Fatal(err)
	}
	blanks := []Blank{item, other}

	if _, found := match_Blank(blanks, "Как зарегистрировать входящий документ?"); found {
		t.Fatal("ordinary question opened a blank")
	}
	if _, found := match_Blank(blanks, "шаблон"); found {
		t.Fatal("the word template alone opened a blank")
	}
	matched, found := match_Blank(blanks, "Нужен модуль о создании тестовой группы")
	if !found || matched.ID != item.ID {
		t.Fatalf("matched %+v %v", matched.Title, found)
	}

	step, handled := advance_Blank(blanks, FillState{}, "Нужен модуль о создании тестовой группы")
	if !handled || step.Ready || step.Field != "1" || step.State.BlankID != item.ID {
		t.Fatalf("start %+v", step)
	}
	step, handled = advance_Blank(blanks, step.State, "  ")
	if !handled || step.Field != "1" {
		t.Fatalf("empty answer %+v", step)
	}
	step, handled = advance_Blank(blanks, step.State, "подготовка")
	if !handled || step.Field != "2" {
		t.Fatalf("second field %+v", step)
	}
	step, handled = advance_Blank(blanks, step.State, "пропустить")
	if !handled || step.Field != "3" {
		t.Fatalf("after skip %+v", step)
	}
	step, handled = advance_Blank(blanks, step.State, "Иванов")
	if !handled || !step.Ready || step.State.BlankID != "" {
		t.Fatalf("ready %+v", step)
	}
	if !strings.Contains(step.DraftText, "подготовка") || !strings.Contains(step.DraftText, "Иванов") {
		t.Fatalf("draft %s", step.DraftText)
	}
	if strings.Contains(step.DraftText, "Лишний") || strings.Contains(step.DraftText, "[1]") || strings.Contains(step.DraftText, "___") {
		t.Fatalf("draft leftovers %s", step.DraftText)
	}
}

func TestCatalog_MatchesWorkGroup(t *testing.T) {
	catalog, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 7 {
		t.Fatalf("catalog %d", len(catalog))
	}
	for _, item := range catalog {
		if item.Title == "" || len(item.Fields) == 0 {
			t.Fatalf("empty blank %+v", item.ID)
		}
	}
	matched, found := Match_Blank("Нужен модуль распоряжения о создании рабочей группы")
	if !found || !strings.Contains(matched.Title, "рабочей группы") || strings.Contains(matched.Title, "комитет") {
		t.Fatalf("work group %+v %v", matched.Title, found)
	}
	if _, found = Match_Blank("Как зарегистрировать входящий документ?"); found {
		t.Fatal("instruction question opened a blank")
	}

	records, found := Match_Blank("Нужен приказ о назначении ответственных за делопроизводство")
	if !found || !strings.Contains(records.Title, "делопроизводство") {
		t.Fatalf("records %+v", records.Title)
	}
	if records.Fields[1].Optional || !field_ByNumber(records, "5").Optional {
		t.Fatalf("records optional %+v", records.Fields)
	}
	committee, found := Match_Blank("Нужен модуль распоряжения о создании проектного комитета")
	if !found || !strings.Contains(committee.Title, "комитет") {
		t.Fatalf("committee %+v", committee.Title)
	}
	if field_ByNumber(committee, "1").Optional || !field_ByNumber(committee, "2").Optional {
		t.Fatalf("committee optional %+v", committee.Fields)
	}

	state := FillState{BlankID: matched.ID, Answers: map[string]string{}}
	for _, field := range matched.Fields {
		state.Answers[field.Number] = "ответ " + field.Number
	}
	draft := Build_Draft(matched, state)
	if !strings.Contains(draft, "ответ 1") || strings.Contains(draft, "[1]") || strings.Contains(draft, "___") {
		t.Fatalf("work group draft %s", draft)
	}
}

func field_ByNumber(item Blank, number string) BlankField {
	for _, field := range item.Fields {
		if field.Number == number {
			return field
		}
	}
	return BlankField{}
}
