package httpapi

import (
	"context"
	"strings"
	"unicode"

	"docfinder/core/internal/chunk"
	"docfinder/core/internal/contacts"
	"docfinder/core/internal/grounding"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

// Справка о документе: сотрудник называет документ целиком («Закон о коммерческой тайне»,
// «что такое Матрица визирования»). Ответ - что это за документ по его началу и кнопка скачивания,
// а не поиск отдельного пункта

type overviewCandidate struct {
	DocumentID string
	Title      string
	Edition    string
	Scope      string
}

// Документ для скачивания под ответом
type replyDownload struct {
	Document   string `json:"document"`
	DocumentID string `json:"documentId"`
	Link       string `json:"link"`
}

var (
	overviewIntentWords   = word_Set(texts.OverviewIntentWords)
	overviewTitleStop     = word_Set(texts.OverviewTitleStopWords)
	overviewDocumentKinds = word_Set(texts.OverviewDocumentKinds)
)

func word_Set(words []string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

func split_OverviewWords(text string) []string {
	folded := strings.ReplaceAll(strings.ToLower(text), "ё", "е")
	return strings.FieldsFunc(folded, func(symbol rune) bool {
		return !unicode.IsLetter(symbol) && !unicode.IsDigit(symbol)
	})
}

// Основа слова без окончания падежа: «коммерческой» и «коммерческая» - «коммерческ»
func stem_OverviewWord(word string) string {
	runes := []rune(word)
	switch {
	case len(runes) > 6:
		return string(runes[:len(runes)-3])
	case len(runes) > 4:
		return string(runes[:len(runes)-2])
	case len(runes) > 3:
		return string(runes[:len(runes)-1])
	}
	return word
}

func check_SameWord(first, second string) bool {
	firstStem := stem_OverviewWord(first)
	secondStem := stem_OverviewWord(second)
	return strings.HasPrefix(first, secondStem) || strings.HasPrefix(second, firstStem)
}

// Вопрос - о документе в целом: в нем большая часть значимых слов названия и нет других значимых слов.
// «Кто визирует письмо в иностранный центробанк?» - вопрос по содержанию Матрицы, а не о ней самой
func match_DocumentOverview(question string, documents []overviewCandidate) (overviewCandidate, bool) {
	questionWords := []string{}
	for _, word := range split_OverviewWords(question) {
		if !overviewIntentWords[word] && !overviewTitleStop[word] {
			questionWords = append(questionWords, word)
		}
	}
	if len(questionWords) == 0 {
		return overviewCandidate{}, false
	}
	var best overviewCandidate
	bestShare := 0.0
	found := false
	for _, document := range documents {
		if contacts.Title_IsDirectory(document.Title) {
			continue
		}
		titleWords := []string{}
		for _, word := range split_OverviewWords(document.Title) {
			if !overviewTitleStop[word] && !overviewIntentWords[word] && !overviewDocumentKinds[word] {
				titleWords = append(titleWords, word)
			}
		}
		if len(titleWords) == 0 {
			continue
		}
		matched := 0
		firstMatched := false
		for index, titleWord := range titleWords {
			for _, questionWord := range questionWords {
				if check_SameWord(titleWord, questionWord) {
					matched++
					firstMatched = firstMatched || index == 0
					break
				}
			}
		}
		// Каждое значимое слово вопроса должно быть из названия
		for _, questionWord := range questionWords {
			inTitle := false
			for _, titleWord := range titleWords {
				if check_SameWord(titleWord, questionWord) {
					inTitle = true
					break
				}
			}
			if !inTitle && !overviewDocumentKinds[questionWord] {
				matched = -1
				break
			}
		}
		if matched <= 0 {
			continue
		}
		share := float64(matched) / float64(len(titleWords))
		// Длинное название называют по началу: «Матрица визирования», «Инструкция по ДОУ».
		// Одно слово из длинного названия («инструкция») - слишком мало, чтобы назвать документ
		if matched == 1 && len(titleWords) > 1 {
			continue
		}
		if share < texts.OverviewTitleShare && !firstMatched {
			continue
		}
		if share > bestShare {
			best, bestShare, found = document, share, true
		}
	}
	return best, found
}

// Справка по началу документа. Модель пишет описание, числа сверяются с текстом.
// Без модели или при неподтвержденном числе - название, редакция и разделы
func build_DocumentOverview(chatter interface {
	Complete_Chat(messages []ollama.ChatMessage) (string, error)
}, question string, document overviewCandidate, opening []livestore.SimilarChunk, sections []string) assistantReply {
	text := ""
	if chatter != nil && len(opening) > 0 {
		openingText := build_OverviewOpening(opening)
		var builder strings.Builder
		builder.WriteString(texts.LiveOverviewTitleLabel + ": " + document.Title + "\n\n")
		builder.WriteString(texts.LiveOverviewOpeningLabel + ":\n" + openingText)
		if len(sections) > 0 {
			builder.WriteString("\n\n" + texts.LiveOverviewSectionsLabel + ": " + strings.Join(sections, "; "))
		}
		answer, err := chatter.Complete_Chat([]ollama.ChatMessage{
			{Role: "system", Content: texts.LiveOverviewPrompt},
			{Role: "user", Content: builder.String()},
		})
		answer = strings.TrimSpace(answer)
		// Числа справки сверяются с тем же текстом, что видела модель
		sources := []string{question, document.Title, document.Edition, openingText, strings.Join(sections, "\n")}
		if err == nil && answer != "" && len(grounding.Find_UnsupportedNumbers(answer, sources)) == 0 {
			text = answer
		}
	}
	if text == "" {
		text = plain_DocumentOverview(document, sections)
	}
	reply := build_PlainReply(texts.KindAnswer, text)
	reply.RelevancePercent = 100
	link := "/documents/" + document.DocumentID + "/file"
	quote := ""
	if len(opening) > 0 {
		quote = opening[0].Content
		if runes := []rune(quote); len(runes) > texts.OverviewQuoteRunes {
			quote = string(runes[:texts.OverviewQuoteRunes]) + "…"
		}
	}
	reply.Citations = []citation{{
		Document:     document.Title,
		Edition:      document.Edition,
		Quote:        quote,
		OriginalLink: link,
		DocumentID:   document.DocumentID,
		Scope:        document.Scope,
	}}
	reply.Download = &replyDownload{Document: document.Title, DocumentID: document.DocumentID, Link: link}
	return reply
}

// Начало документа для модели: первые фрагменты подряд, не длиннее OverviewTextRunes
func build_OverviewOpening(opening []livestore.SimilarChunk) string {
	parts := make([]string, 0, len(opening))
	for _, chunk := range opening {
		parts = append(parts, strings.TrimSpace(chunk.Content))
	}
	text := strings.Join(parts, "\n")
	if runes := []rune(text); len(runes) > texts.OverviewTextRunes {
		text = string(runes[:texts.OverviewTextRunes])
	}
	return text
}

func plain_DocumentOverview(document overviewCandidate, sections []string) string {
	head := strings.ReplaceAll(texts.OverviewPlainIntro, "{title}", document.Title)
	if strings.TrimSpace(document.Edition) != "" {
		head += ", " + strings.ReplaceAll(texts.OverviewPlainEdition, "{edition}", document.Edition)
	}
	lines := []string{head + "."}
	if len(sections) > 0 {
		lines = append(lines, texts.OverviewPlainSections+": "+strings.Join(sections, "; ")+".")
	}
	lines = append(lines, texts.OverviewPlainDownload)
	return strings.Join(lines, "\n")
}

type overviewResult struct {
	Found     bool
	Reply     assistantReply
	Documents []shownDocument
}

// Вопрос о документе среди видимых сотруднику документов выбранных процессов
func (api *liveAPI) find_DocumentOverview(ctx context.Context, token, question, processID string) (overviewResult, error) {
	lines, err := api.store.List_VisibleDocuments(ctx, token, "", processID)
	if err != nil {
		return overviewResult{}, err
	}
	candidates := make([]overviewCandidate, 0, len(lines))
	for _, line := range lines {
		candidates = append(candidates, overviewCandidate{DocumentID: line.DocumentID, Title: line.Title, Edition: line.Edition, Scope: line.Scope})
	}
	document, found := match_DocumentOverview(question, candidates)
	if !found {
		return overviewResult{}, nil
	}
	opening, sections, err := api.store.Read_DocumentOutline(ctx, document.DocumentID, texts.OverviewChunkLimit)
	if err != nil {
		return overviewResult{}, err
	}
	reply := reply_WithContacts(build_DocumentOverview(api.chatter, question, document, opening, sections), nil)
	return overviewResult{
		Found:     true,
		Reply:     reply,
		Documents: []shownDocument{{DocumentID: document.DocumentID, Title: document.Title, Scope: document.Scope}},
	}, nil
}

// Заглушка: справка без модели по видимым документам, разделы - из нарезки файла
func (stubStore *stub) find_DocumentOverview(record *userRecord, question, processID string) overviewResult {
	candidates := []overviewCandidate{}
	byID := map[string]*documentRecord{}
	for _, document := range stubStore.documents {
		if !check_ProcessSelected(processID, document.processID) {
			continue
		}
		if !document_MatchesList("", document.scope, document.ownerID, record.id, document.visibleToAll, document.roleIDs, record.roleIDs) {
			continue
		}
		candidates = append(candidates, overviewCandidate{DocumentID: document.id, Title: document.title, Edition: document.edition, Scope: document.scope})
		byID[document.id] = document
	}
	matched, found := match_DocumentOverview(question, candidates)
	if !found {
		return overviewResult{}
	}
	opening := []livestore.SimilarChunk{}
	sections := []string{}
	seen := map[string]bool{}
	document := byID[matched.DocumentID]
	pieces, _ := chunk.Extract_DocumentChunks(document.fileName, document.body)
	for _, piece := range pieces {
		if len(opening) < texts.OverviewChunkLimit {
			opening = append(opening, livestore.SimilarChunk{DocumentID: document.id, Title: document.title, Edition: document.edition, Section: piece.Section, Paragraph: piece.Paragraph, Appendix: piece.Appendix, Content: piece.Content, Scope: document.scope})
		}
		if section := strings.TrimSpace(piece.Section); section != "" && !seen[section] {
			seen[section] = true
			sections = append(sections, section)
		}
	}
	return overviewResult{
		Found:     true,
		Reply:     reply_WithContacts(build_DocumentOverview(nil, question, matched, opening, sections), nil),
		Documents: []shownDocument{{DocumentID: matched.DocumentID, Title: matched.Title, Scope: matched.Scope}},
	}
}
