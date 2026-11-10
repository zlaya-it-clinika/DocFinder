package blank

import (
	"regexp"
	"strings"

	"docfinder/core/internal/texts"
)

var (
	bracketBeforeMarker = regexp.MustCompile(`\[[^\[\]]*\]\s*$`)
	emptyPoint          = regexp.MustCompile(`^\d+\.$`)
)

type FillState struct {
	BlankID string            `json:"blankId,omitempty"`
	Answers map[string]string `json:"answers,omitempty"`
	Skipped []string          `json:"skipped,omitempty"`
}

type Step struct {
	BlankID string
	Title   string
	// Название модуля для заголовков: «Модуль распоряжения о создании рабочей группы»
	Name       string
	Field      string
	FieldLabel string
	Question   string
	// Все поля без ответа, начиная с текущего: клиент задает их одной карточкой
	Questions []Question
	DraftText string
	DraftDocx []byte
	Ready     bool
	State     FillState
}

func Match_Blank(text string) (Blank, bool) {
	blanks, err := Catalog()
	if err != nil {
		return Blank{}, false
	}
	return match_Blank(blanks, text)
}

// Бланк по коду: первый вопрос. В режиме «План» вид документа выбирают по списку или модель
func Start_Blank(blankID string) (Step, bool) {
	blanks, err := Catalog()
	if err != nil {
		return Step{}, false
	}
	item, found := find_Blank(blanks, blankID)
	if !found {
		return Step{}, false
	}
	state := FillState{BlankID: item.ID}
	field, hasField := first_EmptyField(item, state)
	if !hasField {
		return finish_Blank(item, state), true
	}
	return step_ForField(item, state, field), true
}

func Advance_Blank(state FillState, text string) (Step, bool) {
	blanks, err := Catalog()
	if err != nil {
		return Step{}, false
	}
	return advance_Blank(blanks, state, text)
}

func match_Blank(blanks []Blank, text string) (Blank, bool) {
	query := map[string]bool{}
	for _, token := range tokenize(text) {
		query[token] = true
	}
	bestScore := 0
	secondScore := 0
	var best Blank
	for _, item := range blanks {
		score := 0
		for token := range item.tokens {
			if query[token] {
				score++
			}
		}
		if score > bestScore {
			secondScore = bestScore
			bestScore = score
			best = item
			continue
		}
		if score > secondScore {
			secondScore = score
		}
	}
	if bestScore < 2 || bestScore == secondScore {
		return Blank{}, false
	}
	return best, true
}

func advance_Blank(blanks []Blank, state FillState, text string) (Step, bool) {
	if state.BlankID == "" {
		matched, found := match_Blank(blanks, text)
		if !found {
			return Step{}, false
		}
		state.BlankID = matched.ID
		field, _ := first_EmptyField(matched, state)
		return step_ForField(matched, state, field), true
	}
	current, found := find_Blank(blanks, state.BlankID)
	if !found {
		return Step{}, false
	}
	field, hasField := first_EmptyField(current, state)
	if !hasField {
		return finish_Blank(current, state), true
	}
	answer := strings.TrimSpace(text)
	if answer == "" {
		return step_ForField(current, state, field), true
	}
	if field.Optional && answer_SkipsField(answer) {
		state.Skipped = append(state.Skipped, field.Number)
	} else {
		if state.Answers == nil {
			state.Answers = map[string]string{}
		}
		state.Answers[field.Number] = answer
	}
	next, hasNext := first_EmptyField(current, state)
	if !hasNext {
		return finish_Blank(current, state), true
	}
	return step_ForField(current, state, next), true
}

func answer_SkipsField(answer string) bool {
	lower := strings.ToLower(strings.TrimSpace(answer))
	return lower == texts.BlankSkipNo || lower == texts.BlankSkipPass || lower == texts.BlankSkipDash
}

func find_Blank(blanks []Blank, id string) (Blank, bool) {
	for _, item := range blanks {
		if item.ID == id {
			return item, true
		}
	}
	return Blank{}, false
}

func first_EmptyField(item Blank, state FillState) (BlankField, bool) {
	for _, field := range item.Fields {
		if _, filled := state.Answers[field.Number]; filled {
			continue
		}
		if state_Skips(state, field.Number) {
			continue
		}
		return field, true
	}
	return BlankField{}, false
}

func state_Skips(state FillState, number string) bool {
	for _, skipped := range state.Skipped {
		if skipped == number {
			return true
		}
	}
	return false
}

func step_ForField(item Blank, state FillState, field BlankField) Step {
	return Step{
		BlankID:    item.ID,
		Title:      item.Title,
		Name:       item.Name,
		Field:      field.Number,
		FieldLabel: field.Label,
		Question:   field.Question,
		Questions:  remaining_Questions(item, state),
		State:      state,
	}
}

func finish_Blank(item Blank, state FillState) Step {
	step := Step{
		BlankID:   item.ID,
		Title:     item.Title,
		Name:      item.Name,
		DraftText: Build_Draft(item, state),
		Ready:     true,
		State:     FillState{},
	}
	if docx, err := Fill_ModuleDocx(item.ID, state); err == nil {
		step.DraftDocx = docx
	}
	return step
}

func Build_Draft(item Blank, state FillState) string {
	return clean_Draft(apply_Fields(item.Body, item, state))
}

func apply_Fields(body string, item Blank, state FillState) string {
	for _, field := range item.Fields {
		if state_Skips(state, field.Number) {
			body = skip_Field(body, field.Number)
			continue
		}
		answer, filled := state.Answers[field.Number]
		if !filled {
			continue
		}
		body = apply_Answer(body, field.Number, answer)
	}
	return body
}

func apply_Answer(body, number, answer string) string {
	marker := "[" + number + "]"
	index := strings.Index(body, marker)
	if index < 0 {
		return body
	}
	lineStart := strings.LastIndex(body[:index], "\n") + 1
	spanStart := lineStart
	if previous := strings.LastIndex(body[lineStart:index], "]"); previous >= 0 {
		spanStart = lineStart + previous + 1
	}
	placeholder := strings.Index(body[spanStart:index], "_")
	replaceFrom := index
	if placeholder >= 0 {
		replaceFrom = spanStart + placeholder
	} else {
		replaceFrom = spanStart
	}
	prefix := strings.TrimRight(body[:replaceFrom], " ")
	insertion := answer
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		insertion = " " + answer
	}
	return prefix + insertion + body[index+len(marker):]
}

// Необязательный фрагмент в квадратных скобках стоит сразу перед своим номером.
// Убирается он, а не вся строка, если на ней есть и другие поля.
func skip_Field(body, number string) string {
	marker := "[" + number + "]"
	index := strings.Index(body, marker)
	if index < 0 {
		return body
	}
	lineStart := strings.LastIndex(body[:index], "\n") + 1
	lineEnd := strings.Index(body[index:], "\n")
	if lineEnd < 0 {
		lineEnd = len(body)
	} else {
		lineEnd = index + lineEnd
	}
	line := body[lineStart:lineEnd]
	markerAt := index - lineStart
	if bracket := bracketBeforeMarker.FindStringIndex(line[:markerAt]); bracket != nil {
		updated := strings.TrimSpace(line[:bracket[0]] + line[markerAt+len(marker):])
		if updated == "" || emptyPoint.MatchString(updated) {
			return drop_Line(body, lineStart, lineEnd)
		}
		return body[:lineStart] + updated + body[lineEnd:]
	}
	return drop_Line(body, lineStart, lineEnd)
}

func drop_Line(body string, lineStart, lineEnd int) string {
	end := lineEnd
	if end < len(body) && body[end] == '\n' {
		end++
	}
	return body[:lineStart] + body[end:]
}

func clean_Draft(body string) string {
	withoutMarkers := markerToken.ReplaceAllString(body, "")
	withoutLines := underscoreRun.ReplaceAllString(withoutMarkers, "")
	withoutLines = emptyQuotes.ReplaceAllString(withoutLines, "")
	var kept []string
	blank := false
	for _, line := range strings.Split(withoutLines, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !line_HasWords(trimmed) {
			if len(kept) > 0 && !blank {
				kept = append(kept, "")
				blank = true
			}
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
		blank = false
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, "\n")
}

func line_HasWords(line string) bool {
	for _, symbol := range line {
		if unicodeLetter(symbol) && symbol != 'г' && symbol != 'Г' {
			return true
		}
	}
	return false
}

func unicodeLetter(symbol rune) bool {
	return symbol == 'ё' || symbol == 'Ё' || (symbol >= 'а' && symbol <= 'я') || (symbol >= 'А' && symbol <= 'Я') || (symbol >= 'a' && symbol <= 'z') || (symbol >= 'A' && symbol <= 'Z')
}
