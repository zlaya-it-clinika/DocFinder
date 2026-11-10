package layout

import (
	"strings"

	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

// Реквизиты для внешнего документа, который исправляется по правилам оформления:
// подпись, адресат и заголовок записываются так же, как в макетах

// Подпись должностного лица: должность слева, инициалы и фамилия справа (п. 8.19.2 Инструкции, п. 5.22 ГОСТ)
func Signature_Parts(format Format, official Official) (string, string) {
	signed := sign_Person(format, describe_Official(official), "")
	return signed.Position, signed.Name
}

// Строки адресата: должность и инициалы с фамилией в дательном падеже (п. 8.12.1 Инструкции, п. 5.15 ГОСТ).
// Падеж ставит модель, ответ принимается, только если в нем осталась фамилия. Без модели - как в списке организации
func Addressee_Lines(format Format, official Official, chatter Chatter) []string {
	line := describe_Official(official)
	if chatter != nil {
		answer, err := chatter.Complete_Chat([]ollama.ChatMessage{
			{Role: "system", Content: texts.LayoutDativePrompt},
			{Role: "user", Content: line},
		})
		answer = strings.Trim(strings.TrimSpace(answer), "\".«»")
		surname := strings.ToLower(parse_Person(official.FullName).Surname)
		if err == nil && answer != "" && !strings.Contains(answer, "\n") && surname != "" &&
			strings.Contains(strings.ToLower(strings.ReplaceAll(answer, "ё", "е")), stem_Surname(surname)) {
			line = answer
		}
	}
	return normalize_HeaderLines(format, []string{line}, false)
}

// Заголовок к тексту: с прописной, предлог «О» или «Об» по первой букве (п. 5.17 ГОСТ)
func Title_Text(subject string) string {
	subject = strings.Trim(strings.TrimSpace(subject), ".")
	if subject == "" {
		return ""
	}
	return fix_Preposition(capitalize_First(subject))
}

// Ширина текста на листе: адресат в правой зоне и подпись с фамилией у правого поля
func (format Format) Text_WidthMM() float64 {
	return format.text_WidthMM()
}
