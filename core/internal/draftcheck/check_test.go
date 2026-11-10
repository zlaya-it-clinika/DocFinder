package draftcheck

import (
	"strings"
	"testing"

	"docfinder/core/internal/chunk"
	"docfinder/core/internal/texts"
)

func TestCheck_AgainstReportsMissingRequisites(t *testing.T) {
	chunks := []chunk.TextChunk{
		{Section: "4", Paragraph: "4.1", Content: "Отметка о приложении ставится под текстом документа."},
		{Section: "8", Paragraph: "8.4", Content: "Подпись является обязательным реквизитом и включает должность и фамилию."},
	}
	source := Source{DocumentID: "doc-1", Title: "Инструкция", Edition: "1.0", Scope: "global"}

	missing := Check_Against(chunks, "черновик", source)
	if len(missing) != 2 {
		t.Fatalf("missing %+v", missing)
	}
	if missing[0].Text != texts.DraftRecommendation || missing[0].Paragraph != "4.1" || !strings.Contains(missing[0].Quote, "приложении") {
		t.Fatalf("appendix %+v", missing[0])
	}
	if missing[1].Text != texts.DraftSignatureMissing || missing[1].Paragraph != "8.4" || !strings.Contains(strings.ToLower(missing[1].Quote), "подпись") {
		t.Fatalf("signature %+v", missing[1])
	}
	if missing[0].DocumentID != "doc-1" {
		t.Fatalf("source %+v", missing[0].Source)
	}

	complete := Check_Against(chunks, "Приложение № 1. Подпись Иванова.", source)
	if len(complete) != 0 {
		t.Fatalf("complete %+v", complete)
	}
}

func TestCheck_DraftUsesInstruction(t *testing.T) {
	source := Source{DocumentID: texts.SeedInstructionID, Title: texts.SeedInstructionTitle, Edition: texts.EditionDefault, Scope: texts.ScopeGlobal}
	missing, err := Check_Draft("черновик", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 2 {
		t.Fatalf("missing %+v", missing)
	}
	for _, item := range missing {
		if item.Quote == "" || item.Paragraph == "" {
			t.Fatalf("citation %+v", item)
		}
	}
	complete, err := Check_Draft("К письму приложено приложение. Подпись: Иванов И. И.", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(complete) != 0 {
		t.Fatalf("complete %+v", complete)
	}
}
