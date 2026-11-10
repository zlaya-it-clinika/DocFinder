package httpapi

import (
	"context"
	"log"
	"net/url"
	"strings"

	engineclient "docfinder/core/internal/engine"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

func (api *liveAPI) answer_WithLegalEngine(ctx context.Context, question string, history []chatTurn) (assistantReply, []shownDocument, error) {
	turns := make([]engineclient.Turn, 0, len(history))
	for _, turn := range history {
		turns = append(turns, engineclient.Turn{Role: turn.Role, Text: turn.Text})
	}
	settings := engine_ModelSettings(api.modelClient.Read_Settings())
	answer, err := api.legalEngine.Answer(ctx, question, turns, settings)
	if err != nil {
		return assistantReply{}, nil, err
	}
	reply := assistantReply{
		Kind:      legal_ReplyKind(answer.Kind),
		Text:      strings.TrimSpace(answer.Text),
		Missing:   strings.TrimSpace(answer.Missing),
		Seconds:   answer.Seconds,
		Citations: make([]citation, 0, len(answer.Citations)),
	}
	documents := make([]shownDocument, 0, len(answer.Citations))
	seenOptions := map[string]bool{}
	for _, item := range answer.Citations {
		path := strings.TrimSpace(item.Path)
		link := "/documents/" + url.PathEscape(item.DocID)
		if path != "" {
			link += "?p=" + url.QueryEscape(path)
		}
		reply.Citations = append(reply.Citations, citation{
			Label:        item.Label,
			Document:     item.Document,
			Section:      item.Section,
			Paragraph:    path,
			Quote:        item.Quote,
			OriginalLink: link,
			DocumentID:   item.DocID,
			Scope:        texts.ScopeGlobal,
			Status:       item.Status,
		})
		documents = append(documents, shownDocument{DocumentID: item.DocID, Title: item.Document, Paragraph: path, Scope: texts.ScopeGlobal})
		if reply.Kind == texts.KindClarify && item.Document != "" && !seenOptions[item.Document] {
			seenOptions[item.Document] = true
			reply.Options = append(reply.Options, item.Document)
		}
	}
	return reply, documents, nil
}

func engine_ModelSettings(settings ollama.Settings) engineclient.ModelSettings {
	baseURL := strings.TrimRight(settings.URL, "/")
	apiKey := settings.APIKey
	reranker := settings.RerankModel
	if settings.Provider_OrDefault() == ollama.ProviderOllama {
		baseURL += "/v1"
		apiKey = ""
		reranker = ""
	}
	return engineclient.ModelSettings{
		BaseURL:       baseURL,
		APIKey:        apiKey,
		LLMModel:      settings.ChatModel,
		EmbModel:      settings.EmbedModel,
		RerankerModel: reranker,
		Temperature:   settings.Temperature,
		MaxTokens:     settings.MaxTokens,
	}
}

func legal_ReplyKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case texts.KindAnswer, texts.KindPartial, texts.KindRefusal, texts.KindCorrection,
		texts.KindIllegal, texts.KindOutOfScope, texts.KindClarify:
		return strings.TrimSpace(kind)
	default:
		return texts.KindAnswer
	}
}

func (api *liveAPI) start_EngineReindex() {
	if api.legalEngine == nil {
		return
	}
	go func() {
		ctx := context.Background()
		stored, err := api.store.Export_EngineDocuments(ctx)
		if err != nil {
			log.Printf("legal engine export: %v", err)
			return
		}
		documents := make([]engineclient.Document, 0, len(stored))
		for _, item := range stored {
			documents = append(documents, engineclient.Document{Text: item.Text, FileName: item.FileName, Category: item.Category})
		}
		if err := api.legalEngine.Reindex(ctx, documents); err != nil {
			log.Printf("legal engine reindex: %v", err)
		}
	}()
}
