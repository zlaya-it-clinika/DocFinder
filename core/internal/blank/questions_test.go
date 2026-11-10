package blank

import (
	"strings"
	"testing"
)

// Бланк модуля: все поля сразу, ответы пачкой, «нет» пропускает необязательное поле
func TestAnswer_FieldsFillsBlank(t *testing.T) {
	step, found := Start_Blank("order-work-group")
	if !found || len(step.Questions) == 0 || step.Questions[0].Number != step.Field || !strings.HasPrefix(step.Name, "Модуль") {
		t.Fatalf("start %+v", step)
	}
	answers := []FieldAnswer{}
	for index, question := range step.Questions {
		value := "значение " + question.Number
		// Необязательные поля пропускаются по-разному: пустым ответом и словом «нет»
		if question.Optional {
			value = ""
			if index%2 == 1 {
				value = "нет"
			}
		}
		// Первое поле пустое: его спросят снова
		if index == 0 {
			value = " "
		}
		answers = append(answers, FieldAnswer{Number: question.Number, Value: value})
	}
	next, handled := Answer_Fields(step.State, answers)
	if !handled || next.Ready || next.Field != step.Questions[0].Number || len(next.Questions) != 1 {
		t.Fatalf("first field again %+v", next)
	}
	done, handled := Answer_Fields(next.State, []FieldAnswer{{Number: next.Field, Value: "проверка архива"}})
	if !handled || !done.Ready || !strings.Contains(done.DraftText, "проверка архива") || len(done.DraftDocx) == 0 {
		t.Fatalf("done %+v", done)
	}
	if Field_Label("order-work-group", step.Questions[0].Number) != step.Questions[0].Label || Field_Label("order-work-group", "999") != "" {
		t.Fatal("field labels")
	}
}
