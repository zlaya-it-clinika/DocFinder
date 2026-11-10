package grounding

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	numberToken = regexp.MustCompile(`\d+(?:[.,:/]\d+)*`)
	// Номер шага в списке «1. ...» или «2) ...» - разметка ответа, а не факт из документа
	listMarker = regexp.MustCompile(`(?m)^[ \t*#>-]*\d+[.)][ \t]`)
)

// Числительные словами. В документах число часто стоит словом или рядом с цифрой: «3 (трех)», «за три рабочих дня»
var numberWords = map[string]string{
	"один": "1", "одна": "1", "одно": "1", "одного": "1", "одной": "1", "одному": "1", "одним": "1", "одну": "1",
	"два": "2", "две": "2", "двух": "2", "двум": "2", "двумя": "2",
	"три": "3", "трех": "3", "трем": "3", "тремя": "3",
	"четыре": "4", "четырех": "4", "четырем": "4", "четырьмя": "4",
	"пять": "5", "пяти": "5", "пятью": "5",
	"шесть": "6", "шести": "6", "шестью": "6",
	"семь": "7", "семи": "7", "семью": "7",
	"восемь": "8", "восьми": "8", "восемью": "8",
	"девять": "9", "девяти": "9", "девятью": "9",
	"десять": "10", "десяти": "10", "десятью": "10",
	"пятнадцать": "15", "пятнадцати": "15",
	"двадцать": "20", "двадцати": "20",
	"тридцать": "30", "тридцати": "30",
	"шестьдесят": "60", "шестидесяти": "60",
	"девяносто": "90", "девяноста": "90",
}

// Find_UnsupportedNumbers возвращает числа из ответа, которых нет ни в одном источнике.
// Источники - вопрос и фрагменты вместе с названием документа и номером пункта.
// Пустой результат - каждое число ответа подтверждено источником.
func Find_UnsupportedNumbers(answer string, sources []string) []string {
	known := collect_SourceNumbers(sources)
	unsupported := []string{}
	seen := map[string]bool{}
	for _, token := range numberToken.FindAllString(listMarker.ReplaceAllString(answer, " "), -1) {
		number := normalize_Number(token)
		if number == "" || known[number] || seen[number] {
			continue
		}
		seen[number] = true
		unsupported = append(unsupported, number)
	}
	return unsupported
}

// Числа источников: цифрами и словами. Слова в ответе не проверяются, чтобы «одна из причин» не считалась фактом
func collect_SourceNumbers(sources []string) map[string]bool {
	known := map[string]bool{}
	for _, source := range sources {
		for _, token := range numberToken.FindAllString(source, -1) {
			known[normalize_Number(token)] = true
		}
		words := strings.FieldsFunc(strings.ToLower(source), func(symbol rune) bool {
			return !unicode.IsLetter(symbol)
		})
		for _, word := range words {
			if digits, found := numberWords[strings.ReplaceAll(word, "ё", "е")]; found {
				known[digits] = true
			}
		}
	}
	return known
}

// «5.6.» в конце предложения и «5,6» - то же число, что «5.6»
func normalize_Number(token string) string {
	return strings.ReplaceAll(strings.Trim(token, ".,:/"), ",", ".")
}
