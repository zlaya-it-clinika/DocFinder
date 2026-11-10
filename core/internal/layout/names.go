package layout

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Должность и ФИО из одной строки: «менеджер отдела продаж Петрова Мария Сергеевна» -
// должность «Менеджер отдела продаж», фамилия «Петрова», буквы инициалов «М», «С».
// Как записать инициалы и в каком порядке с фамилией, решают правила оформления (format.go)
type person struct {
	Position string
	// Должность как написана, без прописной в начале: «от менеджера отдела продаж»
	PositionRaw string
	Surname     string
	Initials    []string
	FullName    string
}

func parse_Person(text string) person {
	tokens := strings.Fields(strings.NewReplacer(",", " ", "(", " ", ")", " ").Replace(text))
	end := len(tokens)
	start := end
	// Справа налево: фамилия, имя, отчество с прописной или инициалы, не больше трех слов
	for start > 0 && end-start < 3 {
		token := tokens[start-1]
		if !check_IsNamePart(token) {
			break
		}
		start--
	}
	nameTokens := tokens[start:end]
	// Одно слово с прописной без инициалов - скорее конец должности («отдела ИТ»), чем фамилия
	if len(nameTokens) == 1 && !check_IsInitials(nameTokens[0]) && start > 0 {
		nameTokens = nil
		start = end
	}
	raw := strings.Trim(strings.Join(tokens[:start], " "), " ,.-")
	result := person{Position: capitalize_First(raw), PositionRaw: raw}
	if len(nameTokens) == 0 {
		return result
	}
	result.FullName = strings.Join(nameTokens, " ")
	if check_IsInitials(nameTokens[0]) {
		// «И.О. Фамилия» или «И. О. Фамилия»: фамилия - последнее полное слово
		for _, token := range nameTokens {
			if check_IsInitials(token) {
				result.Initials = append(result.Initials, initial_Letters(token)...)
				continue
			}
			result.Surname = token
		}
		return result
	}
	// «Фамилия Имя Отчество» или «Фамилия И.О.»
	result.Surname = nameTokens[0]
	for _, token := range nameTokens[1:] {
		if check_IsInitials(token) {
			result.Initials = append(result.Initials, initial_Letters(token)...)
			continue
		}
		first, _ := utf8.DecodeRuneInString(token)
		result.Initials = append(result.Initials, string(first))
	}
	return result
}

// «И.О.» -> «И», «О»
func initial_Letters(token string) []string {
	letters := []string{}
	for _, symbol := range token {
		if unicode.IsUpper(symbol) {
			letters = append(letters, string(symbol))
		}
	}
	return letters
}

func check_IsNamePart(token string) bool {
	if check_IsInitials(token) {
		return true
	}
	first, _ := utf8.DecodeRuneInString(token)
	if !unicode.IsUpper(first) || !unicode.Is(unicode.Cyrillic, first) {
		return false
	}
	// Аббревиатура («ИТ», «ДОУ») - часть должности, а не фамилия
	for _, symbol := range token {
		if unicode.IsLetter(symbol) && symbol != first && unicode.IsUpper(symbol) {
			return false
		}
	}
	return true
}

// «И.», «И.О.», «И.И.»
func check_IsInitials(token string) bool {
	letters := 0
	for _, symbol := range token {
		switch {
		case unicode.IsUpper(symbol):
			letters++
		case symbol == '.':
		default:
			return false
		}
	}
	return letters > 0 && letters <= 2 && strings.HasSuffix(token, ".")
}

func capitalize_First(text string) string {
	first, size := utf8.DecodeRuneInString(text)
	if first == utf8.RuneError {
		return text
	}
	return string(unicode.ToUpper(first)) + text[size:]
}
