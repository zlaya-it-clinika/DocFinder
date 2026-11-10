package httpapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"docfinder/core/internal/contacts"
	"docfinder/core/internal/grounding"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

var fragmentNumber = regexp.MustCompile(`\d+`)

type chatTurn struct {
	Role string
	Text string
	// Вид ответа ассистента: по нему видно, что следующая реплика - ответ на уточнение
	Kind string
}

type replyPrompts struct {
	Select  string
	Answer  string
	Clarify string
}

func build_LiveAssistantReply(chatter interface {
	Complete_Chat(messages []ollama.ChatMessage) (string, error)
}, question string, history []chatTurn, visible []livestore.SimilarChunk, closed []livestore.SimilarChunk, prompts replyPrompts) (assistantReply, []shownDocument, error) {
	kind, selected := livestore.Choose_LiveReplyKind(visible, closed)
	if kind == texts.KindFallback {
		return reply_WithContacts(build_PlainReply(texts.KindFallback, texts.FallbackText), nil), []shownDocument{}, nil
	}
	if kind == texts.KindAccessDenied {
		return build_PlainReply(texts.KindAccessDenied, texts.AccessDeniedText), []shownDocument{}, nil
	}
	if chatter == nil {
		return assistantReply{}, nil, livestore.ErrEmbedUnavailable
	}
	selectText, err := chatter.Complete_Chat(build_ChatMessages(prompt_OrText(prompts.Select, texts.LiveSelectPrompt), question, history, selected))
	if err != nil {
		return assistantReply{}, nil, livestore.ErrEmbedUnavailable
	}
	// Пустой список номеров - отказ. Второй вызов модели не делается.
	selected = chunks_AtIndexes(selected, parse_FragmentNumbers(selectText, len(selected)))
	if len(selected) == 0 {
		return reply_WithContacts(build_PlainReply(texts.KindFallback, texts.FallbackText), nil), []shownDocument{}, nil
	}
	// Близкие фрагменты разных документов - еще не повод переспрашивать: часто нужный пункт один,
	// а второй документ лишь похож по словам. Уточняем, только если модели нужны фрагменты разных документов.
	// На ответ пользователя на уточнение второй раз не переспрашиваем
	if kind == texts.KindClarify && count_Documents(selected) > 1 && !answers_Clarify(history) {
		text, err := chatter.Complete_Chat(build_ChatMessages(prompt_OrText(prompts.Clarify, texts.LiveClarifyPrompt), question, history, selected))
		if err != nil || strings.TrimSpace(text) == "" {
			return assistantReply{}, nil, livestore.ErrEmbedUnavailable
		}
		reply := build_PlainReply(texts.KindClarify, strings.TrimSpace(text))
		// Варианты ответа: названия документов, между которыми выбирает пользователь
		reply.Options = document_Titles(selected)
		return reply, []shownDocument{}, nil
	}
	answerPrompt := prompt_OrText(prompts.Answer, texts.LiveAnswerPrompt) + "\n" + texts.LiveAnswerRefusalRule
	text, err := chatter.Complete_Chat(build_ChatMessages(answerPrompt, question, history, selected))
	if err != nil || strings.TrimSpace(text) == "" {
		return assistantReply{}, nil, livestore.ErrEmbedUnavailable
	}
	text = strings.TrimSpace(text)
	// Шаг выбора иногда берет пункт, похожий только словами. Тогда модель ставит метку, и это отказ, а не ответ со ссылкой
	if strings.Contains(strings.ToUpper(text), texts.LiveAnswerNoAnswerMark) {
		return reply_WithContacts(build_PlainReply(texts.KindFallback, texts.FallbackText), nil), []shownDocument{}, nil
	}
	// Число, срок или номер пункта, которых нет ни во фрагментах, ни в вопросе, модель придумала.
	// Пересказ не показываем: отдаем самый близкий из выбранных пунктов дословно
	if unsupported := grounding.Find_UnsupportedNumbers(text, sources_OfChunks(question, selected)); len(unsupported) > 0 {
		selected = selected[:1]
		text = texts.LiveUnsupportedIntro + "\n\n" + selected[0].Content
	}
	reply := build_PlainReply(texts.KindAnswer, text)
	reply.RelevancePercent = livestore.Relevance_Percent(selected[0])
	citations := livestore.Build_ReplyCitations(selected)
	reply.Citations = make([]citation, 0, len(citations))
	documents := make([]shownDocument, 0, len(citations))
	for _, item := range citations {
		reply.Citations = append(reply.Citations, citation{
			Document:     item.Document,
			Edition:      item.Edition,
			Section:      item.Section,
			Paragraph:    item.Paragraph,
			Appendix:     item.Appendix,
			Quote:        item.Quote,
			OriginalLink: item.OriginalLink,
			DocumentID:   item.DocumentID,
			Scope:        item.Scope,
		})
		documents = append(documents, shownDocument{
			DocumentID: item.DocumentID,
			Title:      item.Document,
			Paragraph:  item.Paragraph,
			Scope:      item.Scope,
		})
	}
	return reply_WithContacts(reply, nil), documents, nil
}

// Источники для проверки чисел: вопрос и фрагменты вместе с названием документа и номером пункта
func sources_OfChunks(question string, chunks []livestore.SimilarChunk) []string {
	sources := make([]string, 0, len(chunks)+1)
	sources = append(sources, question)
	for _, chunk := range chunks {
		sources = append(sources, strings.Join([]string{chunk.Title, chunk.Edition, chunk.Section, chunk.Paragraph, chunk.Appendix, chunk.Content}, "\n"))
	}
	return sources
}

func prompt_OrText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parse_FragmentNumbers(raw string, count int) []int {
	trimmed := strings.TrimSpace(raw)
	folded := strings.Trim(strings.ToLower(trimmed), ".,!;: ")
	if trimmed == "" || folded == texts.BlankSkipNo {
		return nil
	}
	matches := fragmentNumber.FindAllString(trimmed, -1)
	if len(matches) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(matches))
	seen := map[int]bool{}
	for _, match := range matches {
		number, convError := strconv.Atoi(match)
		if convError != nil || number < 1 || number > count || seen[number] {
			continue
		}
		seen[number] = true
		indexes = append(indexes, number-1)
	}
	return indexes
}

func count_Documents(chunks []livestore.SimilarChunk) int {
	documents := map[string]bool{}
	for _, chunk := range chunks {
		documents[chunk.DocumentID] = true
	}
	return len(documents)
}

// Названия документов в порядке фрагментов, без повторов
func document_Titles(chunks []livestore.SimilarChunk) []string {
	titles := []string{}
	seen := map[string]bool{}
	for _, chunk := range chunks {
		title := strings.TrimSpace(chunk.Title)
		if title == "" || seen[title] {
			continue
		}
		seen[title] = true
		titles = append(titles, title)
	}
	return titles
}

// Последняя реплика ассистента в истории - уточняющий вопрос
func answers_Clarify(history []chatTurn) bool {
	if len(history) == 0 {
		return false
	}
	last := history[len(history)-1]
	return last.Role == "assistant" && last.Kind == texts.KindClarify
}

// Ответ на уточнение обычно короткий («про исходящие»), и искать по нему одному нельзя.
// Ищем по исходному вопросу вместе с уточнением
func build_ClarifiedQuestion(history []chatTurn, text string) string {
	if !answers_Clarify(history) {
		return text
	}
	for index := len(history) - 2; index >= 0; index-- {
		if history[index].Role == "user" {
			return history[index].Text + "\n" + text
		}
	}
	return text
}

func chunks_AtIndexes(chunks []livestore.SimilarChunk, indexes []int) []livestore.SimilarChunk {
	chosen := map[int]bool{}
	for _, index := range indexes {
		chosen[index] = true
	}
	picked := make([]livestore.SimilarChunk, 0, len(indexes))
	for index, chunk := range chunks {
		if chosen[index] {
			picked = append(picked, chunk)
		}
	}
	return picked
}

func contacts_ForText(text, question string) []replyContact {
	return contacts_FromEntries(contacts.Parse_Entries(text), question)
}

func contacts_FromEntries(entries []contacts.Entry, question string) []replyContact {
	entry, ok := contacts.Choose_Entry(entries, question)
	if !ok {
		return []replyContact{}
	}
	return []replyContact{{Department: entry.Department, Channel: entry.Channel}}
}

func reply_WithContacts(reply assistantReply, list []replyContact) assistantReply {
	if reply.Kind != texts.KindAnswer && reply.Kind != texts.KindFallback {
		return reply
	}
	if list == nil {
		list = []replyContact{}
	}
	reply.Contacts = &list
	return reply
}

func build_ChatMessages(prompt, question string, history []chatTurn, chunks []livestore.SimilarChunk) []ollama.ChatMessage {
	messages := make([]ollama.ChatMessage, 0, len(history)+2)
	messages = append(messages, ollama.ChatMessage{Role: "system", Content: prompt})
	for _, turn := range history {
		messages = append(messages, ollama.ChatMessage{Role: turn.Role, Content: turn.Text})
	}
	messages = append(messages, ollama.ChatMessage{
		Role:    "user",
		Content: format_QuestionWithChunks(question, chunks),
	})
	return messages
}

func format_QuestionWithChunks(question string, chunks []livestore.SimilarChunk) string {
	var builder strings.Builder
	builder.WriteString(texts.LiveQuestionLabel)
	builder.WriteString(": ")
	builder.WriteString(question)
	builder.WriteString("\n\n")
	builder.WriteString(texts.LiveFragmentsLabel)
	builder.WriteString(":\n")
	for index, chunk := range chunks {
		if index > 0 {
			builder.WriteString("\n")
		}
		fmt.Fprintf(&builder, "%d. %s: %s", index+1, texts.LiveDocumentLabel, chunk.Title)
		if chunk.Edition != "" {
			fmt.Fprintf(&builder, ". %s: %s", texts.LiveEditionLabel, chunk.Edition)
		}
		if chunk.Section != "" {
			fmt.Fprintf(&builder, ". %s: %s", texts.LiveSectionLabel, chunk.Section)
		}
		if chunk.Paragraph != "" {
			fmt.Fprintf(&builder, ". %s: %s", texts.LiveParagraphLabel, chunk.Paragraph)
		}
		if chunk.Appendix != "" {
			fmt.Fprintf(&builder, ". %s: %s", texts.LiveAppendixLabel, chunk.Appendix)
		}
		builder.WriteString("\n")
		builder.WriteString(chunk.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}

func read_ChatHistory(rawMessages [][]byte) []chatTurn {
	turns := []chatTurn{}
	for _, raw := range rawMessages {
		var stored struct {
			Role string `json:"role"`
			Text string `json:"text"`
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(raw, &stored); err != nil {
			continue
		}
		if stored.Role != "user" && stored.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(stored.Text) == "" {
			continue
		}
		turns = append(turns, chatTurn{Role: stored.Role, Text: stored.Text, Kind: stored.Kind})
	}
	return turns
}
