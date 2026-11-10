package draftcheck

import (
	"strings"
	"unicode/utf8"

	"docfinder/core/internal/chunk"
	"docfinder/core/internal/texts"
)

type Source struct {
	DocumentID string
	Title      string
	Edition    string
	Scope      string
}

type Recommendation struct {
	Text      string
	Section   string
	Paragraph string
	Appendix  string
	Quote     string
	Source
}

type requisiteRule struct {
	draftNeedle  string
	text         string
	chunkNeedles []string
	quoteNeedle  string
}

func Check_Draft(draftText string, source Source) ([]Recommendation, error) {
	_, chunks, err := instruction_Text()
	if err != nil {
		return nil, err
	}
	return Check_Against(chunks, draftText, source), nil
}

func Check_Against(chunks []chunk.TextChunk, draftText string, source Source) []Recommendation {
	rules := []requisiteRule{
		{draftNeedle: "приложен", text: texts.DraftRecommendation, chunkNeedles: []string{"отмет", "приложен"}, quoteNeedle: "приложен"},
		{draftNeedle: "подпис", text: texts.DraftSignatureMissing, chunkNeedles: []string{"подпись", "реквизит"}, quoteNeedle: "подпись"},
	}
	draft := strings.ToLower(draftText)
	found := make([]Recommendation, 0)
	for _, rule := range rules {
		if strings.Contains(draft, rule.draftNeedle) {
			continue
		}
		piece, ok := find_Chunk(chunks, rule.chunkNeedles)
		if !ok {
			piece, ok = find_Chunk(chunks, []string{rule.quoteNeedle})
		}
		item := Recommendation{Text: rule.text, Source: source}
		if ok {
			item.Section = piece.Section
			item.Paragraph = piece.Paragraph
			item.Appendix = piece.Appendix
			item.Quote = quote_Around(piece.Content, rule.quoteNeedle)
		}
		found = append(found, item)
	}
	return found
}

// В инструкции эти слова встречаются много раз. Берётся самый короткий пункт, где сформулирован сам реквизит.
func find_Chunk(chunks []chunk.TextChunk, needles []string) (chunk.TextChunk, bool) {
	var best chunk.TextChunk
	found := false
	for _, piece := range chunks {
		if !contains_All(piece.Content, needles) {
			continue
		}
		if !found || prefer_Chunk(piece, best) {
			best = piece
			found = true
		}
	}
	return best, found
}

func prefer_Chunk(candidate, current chunk.TextChunk) bool {
	if candidate.Paragraph != "" && current.Paragraph == "" {
		return true
	}
	if candidate.Paragraph == "" && current.Paragraph != "" {
		return false
	}
	return len(candidate.Content) < len(current.Content)
}

func contains_All(text string, needles []string) bool {
	lower := strings.ToLower(text)
	for _, needle := range needles {
		if !strings.Contains(lower, needle) {
			return false
		}
	}
	return true
}

func quote_Around(content, needle string) string {
	cleaned := strings.Join(strings.Fields(content), " ")
	cleaned = strings.Trim(cleaned, " .")
	lower := strings.ToLower(cleaned)
	index := strings.Index(lower, needle)
	if index < 0 {
		return truncate_Runes(cleaned, 400)
	}
	start := utf8.RuneCountInString(cleaned[:index])
	runes := []rune(cleaned)
	from := start - 80
	if from < 0 {
		from = 0
	}
	to := start + utf8.RuneCountInString(needle) + 160
	if to > len(runes) {
		to = len(runes)
	}
	return strings.Trim(string(runes[from:to]), " .,;•")
}

func truncate_Runes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit]))
}
