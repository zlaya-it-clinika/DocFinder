package eval

import (
	_ "embed"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed questions.json
var questionsRaw []byte

type Question struct {
	Question  string `json:"question"`
	Paragraph string `json:"paragraph,omitempty"`
	// Часть названия документа цитаты: пункт засчитывается, только если он из этого документа
	Document string `json:"document,omitempty"`
	Fallback bool   `json:"fallback,omitempty"`
	// Факты, без которых ответ неверен. Варианты одного факта через «|»: "3 |трех|три"
	Facts []string `json:"facts,omitempty"`
}

func Load_Questions() ([]Question, error) {
	var questions []Question
	if err := json.Unmarshal(questionsRaw, &questions); err != nil {
		return nil, err
	}
	return questions, nil
}

func Paragraph_Matches(paragraph, expected string) bool {
	if paragraph == "" || expected == "" {
		return false
	}
	return paragraph == expected || strings.HasPrefix(paragraph, expected+".")
}

// Цитата из нужного пункта и, если задан, из нужного документа
func Citation_Matches(question Question, document, paragraph string) bool {
	if !Paragraph_Matches(paragraph, question.Paragraph) {
		return false
	}
	return question.Document == "" || strings.Contains(fold_Text(document), fold_Text(question.Document))
}

// Каждый факт есть в ответе хотя бы одним из вариантов. Регистр и «ё» не важны.
// Вариант ищется с начала слова: «3 » не находится в «13 дней», а «дательн» находится в «дательном»
func Facts_Match(answer string, facts []string) bool {
	folded := fold_Text(answer)
	for _, fact := range facts {
		found := false
		for _, variant := range strings.Split(fact, "|") {
			if variant != "" && contains_AtWordStart(folded, fold_Text(variant)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func contains_AtWordStart(text, variant string) bool {
	for offset := 0; offset <= len(text); {
		index := strings.Index(text[offset:], variant)
		if index < 0 {
			return false
		}
		start := offset + index
		previous, _ := utf8.DecodeLastRuneInString(text[:start])
		if start == 0 || !(unicode.IsLetter(previous) || unicode.IsDigit(previous)) {
			return true
		}
		_, size := utf8.DecodeRuneInString(text[start:])
		offset = start + size
	}
	return false
}

func fold_Text(text string) string {
	return strings.ReplaceAll(strings.ToLower(text), "ё", "е")
}
