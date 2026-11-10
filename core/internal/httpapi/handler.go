package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"docfinder/core/internal/config"
	engineclient "docfinder/core/internal/engine"
	"docfinder/core/internal/layout"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

const maxJSONBytes = 1 << 20

type errorBody struct {
	Error string `json:"error"`
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (writer *statusWriter) WriteHeader(code int) {
	if writer.wrote {
		return
	}
	writer.status = code
	writer.wrote = true
	writer.ResponseWriter.WriteHeader(code)
}

func (writer *statusWriter) Write(payload []byte) (int, error) {
	if !writer.wrote {
		writer.status = http.StatusOK
		writer.wrote = true
	}
	return writer.ResponseWriter.Write(payload)
}

func NewHandler(loadedConfig config.Config) (http.Handler, error) {
	var routeMux *http.ServeMux
	var routeHandler http.Handler
	// Правила оформления макетов: ошибка в профиле останавливает запуск, а не всплывает в готовом документе
	layoutFormat, err := layout.Load_Format(loadedConfig.LayoutFormatPriority)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(loadedConfig.Mode, config.ModeLive) {
		if strings.TrimSpace(loadedConfig.DatabaseURL) == "" {
			return nil, errors.New(texts.LiveDatabaseMissing)
		}
		embedder := new_ModelClient(loadedConfig)
		store, err := livestore.Open_LiveStore(loadedConfig.DatabaseURL, loadedConfig.OllamaEmbedDimensions, embedder, loadedConfig.AuthDefaults)
		if err != nil {
			return nil, err
		}
		apply_SavedAISettings(store, embedder)
		if err := sync_StandardDocuments(store, loadedConfig.StandardDocsDir); err != nil {
			return nil, err
		}
		// После смены размерности векторов, прерванной переиндексации или загрузки стандартного набора - пересчет в фоне
		store.Start_PendingReindex()
		legalEngine := engineclient.New_Client(loadedConfig.EngineURL, loadedConfig.EngineToken)
		routeMux = register_LiveRoutes(store, embedder, legalEngine, layoutFormat, loadedConfig.LegacyDOUFeatures)
		register_AIRoutes(routeMux, create_LiveAIAdmin(store, embedder))
		register_OrganizationRoutes(routeMux, store)
		register_SourceRoutes(routeMux, store)
		store.Start_SourceLoop()
		routeHandler = routeMux
	} else {
		stubStore := create_StubStore(loadedConfig.StubWithoutUsers, loadedConfig.AuthDefaults, layoutFormat)
		routeMux = stubStore.register_StubRoutes()
		client := new_ModelClient(loadedConfig)
		register_AIRoutes(routeMux, stubStore.create_AIAdmin(client, loadedConfig.OllamaEmbedDimensions))
		register_OrganizationRoutes(routeMux, stubStore.create_Organization())
		register_SourceRoutes(routeMux, stubStore)
		// В боевом режиме ожидающих отсекает хранилище, в заглушке - эта обёртка
		routeHandler = stubStore.reject_PendingUsers(routeMux)
	}
	routeMux.HandleFunc("GET /healthz", func(responseWriter http.ResponseWriter, request *http.Request) {
		write_JSONResponse(responseWriter, http.StatusOK, map[string]string{"status": "ok"})
	})
	register_DocsRoutes(routeMux, create_DocsGate(loadedConfig.DocsPassword))
	return log_RequestsWhenDebug(loadedConfig.Debug, allow_CrossOrigin(routeHandler)), nil
}

func new_ModelClient(loadedConfig config.Config) *ollama.Client {
	client := ollama.New_Client(loadedConfig.OllamaURL, loadedConfig.OllamaEmbedModel, loadedConfig.OllamaChatModel)
	if loadedConfig.AIProvider != ollama.ProviderOpenAI {
		return client
	}
	client.Apply_Settings(ollama.Settings{
		Provider:    ollama.ProviderOpenAI,
		URL:         loadedConfig.LLMBaseURL,
		APIKey:      loadedConfig.LLMAPIKey,
		ChatModel:   loadedConfig.LLMModel,
		EmbedModel:  loadedConfig.EmbedModel,
		RerankModel: loadedConfig.RerankerModel,
	})
	return client
}

func allow_CrossOrigin(nextHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Access-Control-Allow-Origin", "*")
		responseWriter.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		responseWriter.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if request.Method == http.MethodOptions {
			responseWriter.WriteHeader(http.StatusNoContent)
			return
		}
		nextHandler.ServeHTTP(responseWriter, request)
	})
}

func log_RequestsWhenDebug(enabled bool, nextHandler http.Handler) http.Handler {
	if !enabled {
		return nextHandler
	}
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		recorder := &statusWriter{ResponseWriter: responseWriter, status: http.StatusOK}
		nextHandler.ServeHTTP(recorder, request)
		log.Printf("%s %s %d", request.Method, request.URL.Path, recorder.status)
	})
}

func write_JSONResponse(responseWriter http.ResponseWriter, status int, payload any) {
	responseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	responseWriter.WriteHeader(status)
	_ = json.NewEncoder(responseWriter).Encode(payload)
}

func write_ErrorResponse(responseWriter http.ResponseWriter, status int, code string) {
	write_JSONResponse(responseWriter, status, errorBody{Error: code})
}

func read_JSONBody(responseWriter http.ResponseWriter, request *http.Request, decodeTarget any) error {
	request.Body = http.MaxBytesReader(responseWriter, request.Body, maxJSONBytes)
	return json.NewDecoder(request.Body).Decode(decodeTarget)
}

// Настройки из админ-панели важнее .env. Пока их не сохраняли, клиент работает со значениями из .env
func apply_SavedAISettings(store *livestore.LiveStore, client *ollama.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	saved, found, err := store.Read_SavedAISettings(ctx)
	if err != nil {
		log.Printf("ai settings: %v", err)
		return
	}
	if found {
		client.Apply_Settings(saved)
	}
}

func create_LiveAIAdmin(store *livestore.LiveStore, client *ollama.Client) *aiAdmin {
	return &aiAdmin{
		backend:    client,
		dimensions: store.Dimensions(),
		authorize: func(ctx context.Context, token string) error {
			return store.Require_Permission(ctx, token, texts.PermissionAIManage)
		},
		persist:   store.Save_AISettings,
		reindexer: store,
	}
}
