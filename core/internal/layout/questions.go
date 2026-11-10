package layout

import (
	"strings"
	"time"

	"docfinder/core/internal/texts"
)

// Вопросы макета сразу списком: клиент показывает их одной карточкой и присылает ответы пачкой.
// Вопрос по одному (Step.Question) остается для клиентов, которые отвечают обычной репликой

type Question struct {
	Key      string
	Label    string
	Question string
	// Пояснение под вопросом: кто подпишет по умолчанию, как записать должность и ФИО
	Hint     string
	Optional bool
	// Готовые ответы: должностные лица из вкладки «Организация»
	Options []string
}

type FieldAnswer struct {
	Key   string
	Value string
}

// Все поля без ответа по порядку макета
func build_Questions(info texts.LayoutInfo, state State, organization Organization) []Question {
	questions := []Question{}
	for _, field := range info.Fields {
		if _, answered := state.Answers[field.Key]; answered {
			continue
		}
		questions = append(questions, build_FieldQuestion(field, organization))
	}
	return questions
}

func build_FieldQuestion(field texts.LayoutField, organization Organization) Question {
	question := Question{Key: field.Key, Label: field.Label, Question: field.Question, Optional: field.Optional}
	if field.Key != texts.LayoutFieldAddressee && field.Key != texts.LayoutFieldSigner {
		return question
	}
	for _, official := range organization.Officials {
		question.Options = append(question.Options, describe_Official(official))
	}
	if len(organization.Officials) == 0 {
		question.Hint = texts.LayoutOfficialsNone
	}
	// Подписанта можно не указывать: подпишет генеральный директор
	if field.Key == texts.LayoutFieldSigner {
		if chief, ok := find_Chief(organization); ok {
			question.Optional = true
			question.Hint = format_Name(texts.LayoutSignerDefault, describe_Official(chief))
		}
	}
	return question
}

// Ответы пачкой. Неподходящий ответ (пустое обязательное поле) не записывается: поле спросят снова.
// Ответы на уже заполненные и чужие поля не учитываются
func Answer_Fields(state State, answers []FieldAnswer, organization Organization, format Format, chatter Chatter, now time.Time) Step {
	info, found := Find_Layout(state.LayoutID)
	if !found {
		return Step{Cancelled: true}
	}
	if state.Answers == nil {
		state.Answers = map[string]string{}
	}
	for _, field := range info.Fields {
		if _, answered := state.Answers[field.Key]; answered {
			continue
		}
		value, given := find_Answer(answers, field.Key)
		if !given {
			continue
		}
		if answer, accepted := read_Answer(field, value, organization); accepted {
			state.Answers[field.Key] = answer
		}
	}
	return next_Step(info, state, organization, format, chatter, now)
}

// Подпись поля для истории чата: «Адресат», «Суть». Пусто - поля в макете нет
func Field_Label(layoutID, key string) string {
	info, found := Find_Layout(layoutID)
	if !found {
		return ""
	}
	if field, ok := find_Field(info, key); ok {
		return field.Label
	}
	return ""
}

func find_Answer(answers []FieldAnswer, key string) (string, bool) {
	for _, answer := range answers {
		if answer.Key == key {
			return strings.TrimSpace(answer.Value), true
		}
	}
	return "", false
}
