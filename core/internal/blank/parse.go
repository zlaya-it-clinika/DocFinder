package blank

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	titleLine     = regexp.MustCompile(`(?i)^об?\s+\S`)
	noteLine      = regexp.MustCompile(`^(\d+)\.\s*(.*)$`)
	markerToken   = regexp.MustCompile(`\[(\d+)\]`)
	underscoreRun = regexp.MustCompile(`_{2,}`)
	emptyQuotes   = regexp.MustCompile(`«\s*»`)
)

type BlankField struct {
	Number   string
	Label    string
	Question string
	Optional bool
	Note     string
}

type Blank struct {
	ID    string
	Title string
	// Название модуля для списков: из свойств файла (dc:title), иначе заголовок
	Name   string
	Body   string
	Fields []BlankField
	tokens map[string]bool
}

func Parse_BlankText(fileName, plain string) (Blank, error) {
	plain = strings.ReplaceAll(plain, "\r\n", "\n")
	plain = strings.ReplaceAll(plain, "\r", "\n")
	body, notes := split_Notes(plain)
	fields := parse_Fields(body, notes)
	title := find_Title(body)
	id := strings.TrimSuffix(fileName, ".docx")
	return Blank{
		ID:     id,
		Title:  title,
		Name:   title,
		Body:   strings.TrimSpace(body),
		Fields: fields,
		tokens: tokens_Of(title),
	}, nil
}

func split_Notes(plain string) (string, string) {
	lines := strings.Split(plain, "\n")
	for index, line := range lines {
		if strings.Contains(strings.ToLower(line), "примечани") {
			return strings.Join(lines[:index], "\n"), strings.Join(lines[index+1:], "\n")
		}
	}
	return plain, ""
}

func find_Title(body string) string {
	for _, line := range strings.Split(body, "\n") {
		cleaned := clean_Title(line)
		if titleLine.MatchString(cleaned) {
			return cleaned
		}
	}
	return ""
}

func clean_Title(line string) string {
	withoutLines := underscoreRun.ReplaceAllString(line, " ")
	return strings.Join(strings.Fields(withoutLines), " ")
}

func parse_Fields(body, notes string) []BlankField {
	markers := map[string]bool{}
	for _, match := range markerToken.FindAllStringSubmatch(body, -1) {
		markers[match[1]] = true
	}
	noteByNumber := parse_Notes(notes)
	numbers := make([]string, 0, len(noteByNumber))
	for number := range noteByNumber {
		if markers[number] {
			numbers = append(numbers, number)
		}
	}
	sort_Numbers(numbers)
	fields := make([]BlankField, 0, len(numbers))
	for _, number := range numbers {
		note := noteByNumber[number]
		label, question := split_Note(note)
		fields = append(fields, BlankField{
			Number:   number,
			Label:    label,
			Question: question,
			Optional: note_IsOptional(note),
			Note:     note,
		})
	}
	return fields
}

func parse_Notes(notes string) map[string]string {
	found := map[string]string{}
	var number string
	var builder strings.Builder
	flush := func() {
		if number == "" {
			return
		}
		found[number] = strings.TrimSpace(builder.String())
		builder.Reset()
		number = ""
	}
	for _, line := range strings.Split(notes, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Общие правила") || strings.HasPrefix(trimmed, "•") {
			break
		}
		match := noteLine.FindStringSubmatch(trimmed)
		if match == nil {
			if number != "" {
				builder.WriteByte(' ')
				builder.WriteString(trimmed)
			}
			continue
		}
		flush()
		number = match[1]
		builder.WriteString(match[2])
	}
	flush()
	return found
}

func split_Note(note string) (string, string) {
	label, question, found := strings.Cut(note, ".")
	label = strings.TrimSpace(label)
	question = strings.TrimSpace(question)
	if !found || question == "" {
		if label == "" {
			label = strings.TrimSpace(note)
		}
		return label, label
	}
	return label, question
}

func note_IsOptional(note string) bool {
	lower := strings.ToLower(note)
	if strings.Contains(lower, "необязательн") {
		return true
	}
	return strings.Contains(lower, "удалите пункт") ||
		strings.Contains(lower, "удалите и перенумер") ||
		strings.Contains(lower, "удалите всю") ||
		strings.Contains(lower, "иначе удалите")
}

func sort_Numbers(numbers []string) {
	for left := 0; left < len(numbers); left++ {
		for right := left + 1; right < len(numbers); right++ {
			if number_Less(numbers[right], numbers[left]) {
				numbers[left], numbers[right] = numbers[right], numbers[left]
			}
		}
	}
}

func number_Less(left, right string) bool {
	if len(left) != len(right) {
		return len(left) < len(right)
	}
	return left < right
}

func tokens_Of(text string) map[string]bool {
	tokens := map[string]bool{}
	for _, token := range tokenize(text) {
		tokens[token] = true
	}
	return tokens
}

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	var current strings.Builder
	var tokens []string
	flush := func() {
		token := current.String()
		current.Reset()
		if token == "" || token_Skipped(token) {
			return
		}
		tokens = append(tokens, token)
	}
	for _, symbol := range lower {
		if unicode.IsLetter(symbol) || unicode.IsDigit(symbol) {
			current.WriteRune(symbol)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

// Короткие слова вроде «о» и «по» есть почти в каждом заголовке, поэтому в совпадение не входят.
func token_Skipped(token string) bool {
	if len([]rune(token)) < 3 {
		return true
	}
	switch token {
	case "шаблон", "шаблона", "шаблону", "бланк", "бланка", "бланку", "нужен", "нужна", "нужно", "нужны", "модуль", "модуля", "модулю", "модульный", "модульного", "пожалуйста", "документ", "документа":
		return true
	default:
		return false
	}
}
