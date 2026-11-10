package layout

import (
	"strings"
	"unicode"

	"docfinder/core/internal/texts"
)

// Вопросительные слова в начале: «Как составить служебную записку?» - вопрос к базе знаний, а не заказ документа
var questionWords = map[string]bool{
	"как": true, "что": true, "кто": true, "когда": true, "где": true, "какой": true, "какая": true, "какие": true,
	"какое": true, "каким": true, "сколько": true, "можно": true, "зачем": true, "почему": true, "чем": true, "ли": true,
}

// Слово «нужен» тоже просьба, но «нужно ли указывать ...» - вопрос
var needWords = map[string]bool{"нужен": true, "нужна": true, "нужно": true, "нужны": true}

// Акт ищем целым словом: корень «акт» есть в «актуальный» и «активный»
var actWords = map[string]bool{"акт": true, "акта": true, "акту": true, "актом": true}

func Find_Layout(id string) (texts.LayoutInfo, bool) {
	for _, info := range texts.Layout_Catalog() {
		if info.ID == id {
			return info, true
		}
	}
	return texts.LayoutInfo{}, false
}

// Просьба подготовить документ: вид документа и глагол просьбы, но не вопрос «как оформить ...?»
func Match_Request(text string) (texts.LayoutInfo, bool) {
	if !Check_IsRequest(text) {
		return texts.LayoutInfo{}, false
	}
	return Match_Document(text)
}

// Глагол просьбы («подготовь», «напиши», «нужна»), но не вопрос «как оформить ...?»
func Check_IsRequest(text string) bool {
	words := split_Words(text)
	return len(words) > 0 && check_IsRequest(text, words)
}

// Вид документа, названный в реплике, без проверки просьбы: в режиме «План» каждая реплика - про документ
func Match_Document(text string) (texts.LayoutInfo, bool) {
	words := split_Words(text)
	if len(words) == 0 {
		return texts.LayoutInfo{}, false
	}
	// Закупка - тоже служебная записка, поэтому проверяется раньше: «служебка на закупку ноутбуков»
	catalog := texts.Layout_Catalog()
	ordered := make([]texts.LayoutInfo, 0, len(catalog))
	for _, info := range catalog {
		if info.ID == "purchase-request" {
			ordered = append([]texts.LayoutInfo{info}, ordered...)
			continue
		}
		ordered = append(ordered, info)
	}
	for _, info := range ordered {
		if layout_MatchesWords(info, words) {
			return info, true
		}
	}
	return texts.LayoutInfo{}, false
}

func layout_MatchesWords(info texts.LayoutInfo, words []string) bool {
	for _, keyword := range info.Keywords {
		for _, word := range words {
			if strings.HasSuffix(keyword, " ") {
				if actWords[word] {
					return true
				}
				continue
			}
			if strings.HasPrefix(word, keyword) {
				return true
			}
		}
	}
	return false
}

func check_IsRequest(text string, words []string) bool {
	if questionWords[words[0]] && strings.HasSuffix(strings.TrimSpace(text), "?") {
		return false
	}
	for index, word := range words {
		if needWords[word] && (index+1 >= len(words) || words[index+1] != "ли") {
			return true
		}
		for _, root := range strings.Fields(texts.LayoutIntentRoots) {
			if strings.HasPrefix(word, root) {
				return true
			}
		}
	}
	return false
}

// «отмена», «стоп»: остановить подготовку документа
func Check_IsCancel(text string) bool {
	words := split_Words(text)
	if len(words) == 0 || len(words) > 3 {
		return false
	}
	for _, word := range strings.Fields(texts.LayoutCancelWords) {
		if words[0] == word {
			return true
		}
	}
	return false
}

// «нет», «-», «пропустить» - необязательное поле пропущено
func check_IsSkip(text string) bool {
	folded := strings.Trim(strings.ToLower(strings.TrimSpace(text)), ".,!;: ")
	return folded == "" || folded == texts.BlankSkipNo || folded == texts.BlankSkipPass || folded == texts.BlankSkipDash
}

func split_Words(text string) []string {
	folded := strings.ReplaceAll(strings.ToLower(text), "ё", "е")
	return strings.FieldsFunc(folded, func(symbol rune) bool {
		return !unicode.IsLetter(symbol) && !unicode.IsDigit(symbol) && symbol != '-'
	})
}
