package contacts

import "testing"

func TestTitle_IsDirectory(t *testing.T) {
	if !Title_IsDirectory("Контакты подразделений") {
		t.Fatal("contacts title")
	}
	if !Title_IsDirectory("Справочник ответственных подразделений") {
		t.Fatal("directory title")
	}
	if Title_IsDirectory("Инструкция по документообороту") || Title_IsDirectory("Справочник бланков") {
		t.Fatal("instruction is not a directory")
	}
}

func TestParse_EntriesAndChoose(t *testing.T) {
	text := "Центр документационного обеспечения\nтелефон 1-10-20\n\nУправление информационных технологий\nпочта wifi@bank\n"
	entries := Parse_Entries(text)
	if len(entries) != 2 || entries[0].Department != "Центр документационного обеспечения" || entries[0].Channel != "телефон 1-10-20" {
		t.Fatalf("entries %+v", entries)
	}
	if entries[1].Channel != "почта wifi@bank" {
		t.Fatalf("channel %+v", entries[1])
	}
	matched, ok := Choose_Entry(entries, "Куда писать на wifi")
	if !ok || matched.Department != "Управление информационных технологий" {
		t.Fatalf("matched %+v %v", matched, ok)
	}
	first, ok := Choose_Entry(entries, "Как зарегистрировать входящий")
	if !ok || first.Department != "Центр документационного обеспечения" {
		t.Fatalf("first %+v %v", first, ok)
	}
	if _, ok := Choose_Entry(nil, "вопрос"); ok {
		t.Fatal("empty directory")
	}
}
