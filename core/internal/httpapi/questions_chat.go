package httpapi

import (
	"strings"

	"docfinder/core/internal/blank"
	"docfinder/core/internal/layout"
)

// Вопросы документа одной карточкой: ядро отдает все поля без ответа (template.questions),
// клиент присылает ответы пачкой (answers). Подписи полей в истории берутся из ядра, а не от клиента

type templateQuestion struct {
	Field    string   `json:"field"`
	Label    string   `json:"label"`
	Question string   `json:"question"`
	Hint     string   `json:"hint,omitempty"`
	Optional bool     `json:"optional,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// Ответ на поле из сообщения клиента
type fieldAnswer struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// Ответ на поле в истории сессии: подпись поля и что ответил сотрудник
type answerLine struct {
	Field string `json:"field"`
	Label string `json:"label"`
	Value string `json:"value"`
}

func questions_FromLayout(questions []layout.Question) []templateQuestion {
	result := make([]templateQuestion, 0, len(questions))
	for _, question := range questions {
		result = append(result, templateQuestion{
			Field: question.Key, Label: question.Label, Question: question.Question,
			Hint: question.Hint, Optional: question.Optional, Options: question.Options,
		})
	}
	return result
}

func questions_FromBlank(questions []blank.Question) []templateQuestion {
	result := make([]templateQuestion, 0, len(questions))
	for _, question := range questions {
		result = append(result, templateQuestion{Field: question.Number, Label: question.Label, Question: question.Question, Optional: question.Optional})
	}
	return result
}

func layout_Answers(answers []fieldAnswer) []layout.FieldAnswer {
	result := make([]layout.FieldAnswer, 0, len(answers))
	for _, answer := range answers {
		result = append(result, layout.FieldAnswer{Key: answer.Field, Value: answer.Value})
	}
	return result
}

func blank_Answers(answers []fieldAnswer) []blank.FieldAnswer {
	result := make([]blank.FieldAnswer, 0, len(answers))
	for _, answer := range answers {
		result = append(result, blank.FieldAnswer{Number: answer.Field, Value: answer.Value})
	}
	return result
}

// Ответы для истории: только поля документа, подпись - из ядра. label(field) пустой - поля нет
func answer_Lines(answers []fieldAnswer, label func(field string) string) []answerLine {
	lines := []answerLine{}
	for _, answer := range answers {
		fieldLabel := label(answer.Field)
		if fieldLabel == "" {
			continue
		}
		lines = append(lines, answerLine{Field: answer.Field, Label: fieldLabel, Value: strings.TrimSpace(answer.Value)})
	}
	return lines
}
