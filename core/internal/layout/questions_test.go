package layout

import (
	"strings"
	"testing"

	"docfinder/core/internal/texts"
)

// Все вопросы макета сразу и ответы пачкой: пустое обязательное поле спрашивается снова, остальное принимается
func TestAnswer_FieldsInOneTurn(t *testing.T) {
	organization := test_Organization(t)
	format := test_Format(t)
	info, _ := Find_Layout("letter")
	step := Start_Layout(info, "Сделай письмо о предоставлении данных", organization, format, nil, testNow)
	if len(step.Questions) != 5 || step.Questions[0].Key != texts.LayoutFieldAddressee || len(step.Questions[0].Options) != 2 {
		t.Fatalf("questions %+v", step.Questions)
	}
	var signer Question
	for _, question := range step.Questions {
		if question.Key == texts.LayoutFieldSigner {
			signer = question
		}
	}
	// Подписанта можно не указывать: подпишет генеральный директор
	if !signer.Optional || !strings.Contains(signer.Hint, "Петров Андрей Борисович") {
		t.Fatalf("signer %+v", signer)
	}

	step = Answer_Fields(step.State, []FieldAnswer{
		{Key: texts.LayoutFieldAddressee, Value: "Руководитель ИТ-отдела Иванов И.И."},
		{Key: texts.LayoutFieldPoints, Value: "  "},
		{Key: texts.LayoutFieldSigner, Value: ""},
		{Key: texts.LayoutFieldAttach, Value: "нет"},
		{Key: texts.LayoutFieldExecutor, Value: "Петрова Мария Сергеевна, +7 (495) 123-45-67"},
		{Key: "unknown", Value: "лишнее"},
	}, organization, format, nil, testNow)
	if step.Ready || step.Field != texts.LayoutFieldPoints || len(step.Questions) != 1 {
		t.Fatalf("required points must be asked again %+v", step)
	}
	if step.State.Answers[texts.LayoutFieldSigner] != "Генеральный директор Петров Андрей Борисович" || step.State.Answers[texts.LayoutFieldAttach] != "" {
		t.Fatalf("answers %+v", step.State.Answers)
	}
	step = Answer_Fields(step.State, []FieldAnswer{{Key: texts.LayoutFieldPoints, Value: "Просим направить данные до 01.09.2026."}}, organization, format, nil, testNow)
	if !step.Ready || len(step.Docx) == 0 || !strings.Contains(step.DraftText, "Просим направить данные до 01.09.2026.") {
		t.Fatalf("ready %+v", step)
	}
	if Field_Label("letter", texts.LayoutFieldPoints) != "Суть" || Field_Label("letter", "unknown") != "" {
		t.Fatal("field labels")
	}
}
