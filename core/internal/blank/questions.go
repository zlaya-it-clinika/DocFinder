package blank

import "strings"

// Вопросы бланка сразу списком: клиент показывает их одной карточкой и присылает ответы пачкой

type Question struct {
	Number   string
	Label    string
	Question string
	Optional bool
}

type FieldAnswer struct {
	Number string
	Value  string
}

// Поля без ответа и не пропущенные, по порядку бланка
func remaining_Questions(item Blank, state FillState) []Question {
	questions := []Question{}
	for _, field := range item.Fields {
		if _, filled := state.Answers[field.Number]; filled || state_Skips(state, field.Number) {
			continue
		}
		questions = append(questions, Question{Number: field.Number, Label: field.Label, Question: field.Question, Optional: field.Optional})
	}
	return questions
}

// Ответы пачкой: пустой ответ или «нет» у необязательного поля - пропуск, пустой обязательный - поле спросят снова
func Answer_Fields(state FillState, answers []FieldAnswer) (Step, bool) {
	blanks, err := Catalog()
	if err != nil {
		return Step{}, false
	}
	current, found := find_Blank(blanks, state.BlankID)
	if !found {
		return Step{}, false
	}
	for _, field := range current.Fields {
		if _, filled := state.Answers[field.Number]; filled || state_Skips(state, field.Number) {
			continue
		}
		value, given := find_Answer(answers, field.Number)
		if !given {
			continue
		}
		// Пустой ответ или «нет» у необязательного поля - пропуск, пустой обязательный - спросим снова
		if field.Optional && (value == "" || answer_SkipsField(value)) {
			state.Skipped = append(state.Skipped, field.Number)
			continue
		}
		if value == "" {
			continue
		}
		if state.Answers == nil {
			state.Answers = map[string]string{}
		}
		state.Answers[field.Number] = value
	}
	next, hasNext := first_EmptyField(current, state)
	if !hasNext {
		return finish_Blank(current, state), true
	}
	return step_ForField(current, state, next), true
}

// Подпись поля для истории чата. Пусто - поля в бланке нет
func Field_Label(blankID, number string) string {
	blanks, err := Catalog()
	if err != nil {
		return ""
	}
	item, found := find_Blank(blanks, blankID)
	if !found {
		return ""
	}
	for _, field := range item.Fields {
		if field.Number == number {
			return field.Label
		}
	}
	return ""
}

func find_Answer(answers []FieldAnswer, number string) (string, bool) {
	for _, answer := range answers {
		if answer.Number == number {
			return strings.TrimSpace(answer.Value), true
		}
	}
	return "", false
}
