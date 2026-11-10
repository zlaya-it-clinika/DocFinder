package draftassist

import (
	"strings"
	"testing"
	"unicode/utf8"

	"docfinder/core/internal/chunk"
)

func TestSplit_DraftTextPacksParagraphs(t *testing.T) {
	short := "Первый абзац.\n\nВторой абзац."
	parts := Split_DraftText(short)
	if len(parts) != 1 || parts[0] != short {
		t.Fatalf("short %#v", parts)
	}
	left := strings.Repeat("а", 4000)
	right := strings.Repeat("б", 4000)
	long := left + "\n\n" + right
	parts = Split_DraftText(long)
	if len(parts) != 2 || parts[0] != left || parts[1] != right {
		t.Fatalf("packed %d", len(parts))
	}
	if Join_DraftParts(parts) != long {
		t.Fatal("join")
	}
	huge := strings.Repeat("в", PartRuneLimit+5)
	parts = Split_DraftText(huge)
	if len(parts) != 2 || utf8.RuneCountInString(parts[0]) != PartRuneLimit || utf8.RuneCountInString(parts[1]) != 5 {
		t.Fatalf("hard split %d %d", utf8.RuneCountInString(parts[0]), utf8.RuneCountInString(parts[1]))
	}
	if Join_DraftParts(parts) != parts[0]+"\n\n"+parts[1] {
		t.Fatal("hard join")
	}
}

func TestParse_PartResultReadsJSON(t *testing.T) {
	parsed, err := Parse_PartResult("```json\n{\"draftText\":\"Новый текст\",\"changes\":[{\"text\":\"Добавлен вывод\",\"paragraph\":\"3.2\"}]}\n```")
	if err != nil || parsed.DraftText != "Новый текст" || len(parsed.Changes) != 1 || parsed.Changes[0].Paragraph != "3.2" {
		t.Fatalf("%+v %v", parsed, err)
	}
	emptyChanges, err := Parse_PartResult(`{"draftText":"тот же","changes":[]}`)
	if err != nil || emptyChanges.DraftText != "тот же" || len(emptyChanges.Changes) != 0 {
		t.Fatalf("empty changes %+v %v", emptyChanges, err)
	}
	if _, err = Parse_PartResult("не json"); err == nil {
		t.Fatal("invalid json accepted")
	}
	if _, err = Parse_PartResult(`{"draftText":"  ","changes":[]}`); err == nil {
		t.Fatal("empty draft accepted")
	}
}

func TestBuild_DocxRoundTrip(t *testing.T) {
	body, err := Build_Docx("Первая строка\nВторая строка\n\na < b & c")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 2 || body[0] != 'P' || body[1] != 'K' {
		t.Fatal("not a zip")
	}
	plain, err := chunk.Extract_DocumentText("draft.docx", body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "Первая строка") || !strings.Contains(plain, "Вторая строка") || !strings.Contains(plain, "a < b & c") {
		t.Fatalf("extracted %q", plain)
	}
}
