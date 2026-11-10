package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	engineclient "docfinder/core/internal/engine"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

func TestEngineModelSettings(t *testing.T) {
	ollamaSettings := engine_ModelSettings(ollama.Settings{Provider: ollama.ProviderOllama, URL: "http://ollama:11434/", ChatModel: "chat", EmbedModel: "embed", APIKey: "ignored", RerankModel: "ignored"})
	if ollamaSettings.BaseURL != "http://ollama:11434/v1" || ollamaSettings.APIKey != "" || ollamaSettings.RerankerModel != "" {
		t.Fatalf("ollama settings %+v", ollamaSettings)
	}
	openAISettings := engine_ModelSettings(ollama.Settings{Provider: ollama.ProviderOpenAI, URL: "https://api.example/v1", APIKey: "secret", ChatModel: "chat", EmbedModel: "embed", RerankModel: "rerank"})
	if openAISettings.BaseURL != "https://api.example/v1" || openAISettings.APIKey != "secret" || openAISettings.RerankerModel != "rerank" {
		t.Fatalf("openai settings %+v", openAISettings)
	}
}

func TestLegalEngineReplyMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(response).Encode(engineclient.Answer{
			Kind: "partial", Text: "Найденная часть ответа", Missing: "Нет приложения", Seconds: 1.25,
			Citations: []engineclient.Citation{{Label: "F1", DocID: "contract", Document: "Договор № 1", Path: "4.2", Quote: "30 дней", Status: "действующая"}},
		})
	}))
	defer server.Close()

	model := ollama.New_Client("http://ollama:11434", "bge-m3", "chat")
	api := &liveAPI{legalEngine: engineclient.New_Client(server.URL, ""), modelClient: model}
	reply, documents, err := api.answer_WithLegalEngine(context.Background(), "Вопрос", []chatTurn{{Role: "user", Text: "Ранее"}})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Kind != texts.KindPartial || reply.Missing != "Нет приложения" || reply.Seconds != 1.25 || len(reply.Citations) != 1 {
		t.Fatalf("reply %+v", reply)
	}
	if reply.Citations[0].OriginalLink != "/documents/contract?p=4.2" || reply.Citations[0].Status != "действующая" {
		t.Fatalf("citation %+v", reply.Citations[0])
	}
	if len(documents) != 1 || documents[0].DocumentID != "contract" {
		t.Fatalf("documents %+v", documents)
	}
}
