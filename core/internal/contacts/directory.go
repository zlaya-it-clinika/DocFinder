package contacts

import (
	"strings"
	"unicode/utf8"
)

const (
	titleContactMark    = "контакт"
	titleDirectoryMark  = "справочник"
	titleDepartmentMark = "подраздел"
)

type Entry struct {
	Department string
	Channel    string
}

func Title_IsDirectory(title string) bool {
	folded := strings.ToLower(title)
	if strings.Contains(folded, titleContactMark) {
		return true
	}
	return strings.Contains(folded, titleDirectoryMark) && strings.Contains(folded, titleDepartmentMark)
}

func Title_SQLCondition() string {
	return `(strpos(lower(d.title), '` + titleContactMark + `') > 0 OR (strpos(lower(d.title), '` + titleDirectoryMark + `') > 0 AND strpos(lower(d.title), '` + titleDepartmentMark + `') > 0))`
}

func Parse_Entries(text string) []Entry {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	blocks := strings.Split(normalized, "\n\n")
	entries := make([]Entry, 0, len(blocks))
	for _, block := range blocks {
		entry, ok := entry_FromBlock(block)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func entry_FromBlock(block string) (Entry, bool) {
	lines := strings.Split(strings.TrimSpace(block), "\n")
	department := ""
	channelLines := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if department == "" {
			department = trimmed
			continue
		}
		channelLines = append(channelLines, trimmed)
	}
	if department == "" {
		return Entry{}, false
	}
	return Entry{Department: department, Channel: strings.Join(channelLines, " ")}, true
}

// Нет пересечения слов с вопросом - первая запись справочника.
func Choose_Entry(entries []Entry, question string) (Entry, bool) {
	if len(entries) == 0 {
		return Entry{}, false
	}
	tokens := question_Tokens(question)
	bestIndex := 0
	bestScore := 0
	for index, entry := range entries {
		score := token_Overlap(tokens, entry)
		if score > bestScore {
			bestScore = score
			bestIndex = index
		}
	}
	return entries[bestIndex], true
}

func question_Tokens(question string) []string {
	tokens := []string{}
	for _, word := range strings.Fields(strings.ToLower(question)) {
		if utf8.RuneCountInString(word) < 3 {
			continue
		}
		tokens = append(tokens, word)
	}
	return tokens
}

func token_Overlap(tokens []string, entry Entry) int {
	haystack := strings.ToLower(entry.Department + " " + entry.Channel)
	score := 0
	for _, token := range tokens {
		if strings.Contains(haystack, token) {
			score++
		}
	}
	return score
}
