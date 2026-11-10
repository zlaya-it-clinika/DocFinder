package blank

import (
	"strings"
	"testing"

	"docfinder/core/internal/chunk"
)

func TestFill_ModuleDocxKeepsAnswerAndDropsNotes(t *testing.T) {
	name, body, err := Read_ModuleFile("order-work-group")
	if err != nil || name != "order-work-group.docx" || !strings.HasPrefix(string(body), "PK") {
		t.Fatalf("module %s %v", name, err)
	}
	filled, err := Fill_ModuleDocx("order-work-group", FillState{
		BlankID: "order-work-group",
		Answers: map[string]string{"1": "подготовка отчёта"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(filled), "PK") {
		t.Fatal("filled file is not a docx")
	}
	plain, err := chunk.Extract_DocumentText(name, filled)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "подготовка отчёта") || strings.Contains(strings.ToLower(plain), "примечани") || strings.Contains(plain, "[1]") {
		t.Fatalf("filled text %s", plain)
	}
}
