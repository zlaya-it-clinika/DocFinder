package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"docfinder/core/internal/livestore"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

// Настройка локальной модели из админ-панели, как «Подключения» и «Модели» в open-webui.
// Обработчики общие для боевого режима и заглушки: разница только в backend, правах и хранении

type aiBackend interface {
	Read_Settings() ollama.Settings
	Apply_Settings(settings ollama.Settings)
	Check_Version(baseURL string) (string, error)
	Check_Server(settings ollama.Settings) (string, error)
	List_Models() ([]ollama.ModelInfo, error)
	Pull_Model(ctx context.Context, name string, progress func(ollama.PullProgress)) error
	Delete_Model(name string) error
	Embed_WithModel(baseURL, model string, inputs []string) ([][]float32, error)
	Embed_WithSettings(settings ollama.Settings, inputs []string) ([][]float32, error)
	Complete_ChatWithModel(model string, messages []ollama.ChatMessage) (string, error)
}

type aiReindexer interface {
	Start_Reindex(ctx context.Context, token string) (livestore.ReindexStatus, error)
	Read_ReindexStatus(ctx context.Context, token string) (livestore.ReindexStatus, error)
}

type aiAdmin struct {
	backend    aiBackend
	dimensions int
	// Проверка флага ai.manage. Ошибки - из livestore: ErrUnauthorized, ErrAccessDenied, ErrAccountPending
	authorize func(ctx context.Context, token string) error
	persist   func(ctx context.Context, token string, settings ollama.Settings) error
	reindexer aiReindexer
	pull      pullState
}

// Одно скачивание за раз, как очередь моделей в open-webui, только без очереди
type pullState struct {
	mu        sync.Mutex
	model     string
	status    string
	completed int64
	total     int64
	running   bool
	failed    bool
	cancel    context.CancelFunc
}

type aiSettingsResponse struct {
	ollama.Settings
	// Ключ API сохранён на сервере. Сам ключ клиенту не отдаётся
	APIKeySet       bool               `json:"apiKeySet"`
	Providers       []string           `json:"providers"`
	EmbedDimensions int                `json:"embedDimensions"`
	ReindexRequired bool               `json:"reindexRequired"`
	Limits          ollama.ParamLimits `json:"limits"`
}

func build_AISettingsResponse(settings ollama.Settings, dimensions int, reindexRequired bool) aiSettingsResponse {
	settings.Provider = settings.Provider_OrDefault()
	return aiSettingsResponse{
		Settings:        settings.Without_Secret(),
		APIKeySet:       settings.APIKey != "",
		Providers:       []string{ollama.ProviderOllama, ollama.ProviderOpenAI},
		EmbedDimensions: dimensions,
		ReindexRequired: reindexRequired,
		Limits:          ollama.Read_ParamLimits(),
	}
}

// Модель в списке и для чего она сейчас используется. Сравнение имён с тегом :latest делает ядро
type modelLine struct {
	ollama.ModelInfo
	UsedForChat  bool `json:"usedForChat"`
	UsedForEmbed bool `json:"usedForEmbed"`
}

type pullResponse struct {
	Model     string `json:"model"`
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	Running   bool   `json:"running"`
	Failed    bool   `json:"failed"`
}

type reindexResponse struct {
	Running    bool   `json:"running"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	Failed     bool   `json:"failed"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

type modelNameRequest struct {
	Name string `json:"name"`
}

func register_AIRoutes(routeMux *http.ServeMux, admin *aiAdmin) {
	routeMux.HandleFunc("GET /ai/settings", admin.serve_Settings)
	routeMux.HandleFunc("POST /ai/settings", admin.serve_SaveSettings)
	routeMux.HandleFunc("POST /ai/verify", admin.serve_Verify)
	routeMux.HandleFunc("GET /ai/models", admin.serve_Models)
	routeMux.HandleFunc("GET /ai/models/pull", admin.serve_PullStatus)
	routeMux.HandleFunc("POST /ai/models/pull", admin.serve_StartPull)
	routeMux.HandleFunc("POST /ai/models/pull/cancel", admin.serve_CancelPull)
	routeMux.HandleFunc("POST /ai/models/delete", admin.serve_DeleteModel)
	routeMux.HandleFunc("POST /ai/models/test", admin.serve_TestModel)
	routeMux.HandleFunc("GET /ai/reindex", admin.serve_ReindexStatus)
	routeMux.HandleFunc("POST /ai/reindex", admin.serve_StartReindex)
}

// Токен и флаг ai.manage. false - ответ с ошибкой уже записан
func (admin *aiAdmin) require_Manager(responseWriter http.ResponseWriter, request *http.Request) (string, bool) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return "", false
	}
	if err := admin.authorize(request.Context(), token); err != nil {
		write_AIError(responseWriter, err)
		return "", false
	}
	return token, true
}

func (admin *aiAdmin) serve_Settings(responseWriter http.ResponseWriter, request *http.Request) {
	if _, ok := admin.require_Manager(responseWriter, request); !ok {
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_AISettingsResponse(admin.backend.Read_Settings(), admin.dimensions, false))
}

// Сохранение. Новая модель эмбеддингов сначала проверяется: её вектор должен совпасть с размером индекса.
// Пустой ключ API при том же провайдере - ключ не меняется (форма не получает сохранённый ключ)
func (admin *aiAdmin) serve_SaveSettings(responseWriter http.ResponseWriter, request *http.Request) {
	var body ollama.Settings
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	current := admin.backend.Read_Settings()
	if strings.TrimSpace(body.APIKey) == "" && body.Provider_OrDefault() == current.Provider_OrDefault() {
		body.APIKey = current.APIKey
	}
	settings, valid := body.Normalize()
	if !valid {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, ok := admin.require_Manager(responseWriter, request)
	if !ok {
		return
	}
	embedChanged := settings.EmbedModel != current.EmbedModel || settings.URL != current.URL ||
		settings.Provider != current.Provider_OrDefault() || settings.APIKey != current.APIKey
	if embedChanged {
		if err := admin.check_EmbedModel(settings); err != nil {
			write_AIError(responseWriter, err)
			return
		}
	}
	if err := admin.persist(request.Context(), token, settings); err != nil {
		write_AIError(responseWriter, err)
		return
	}
	admin.backend.Apply_Settings(settings)
	// Векторы старой модели с новой несравнимы: базу нужно переиндексировать
	reindex := settings.EmbedModel != current.EmbedModel || settings.Provider != current.Provider_OrDefault()
	write_JSONResponse(responseWriter, http.StatusOK, build_AISettingsResponse(settings, admin.dimensions, reindex))
}

// Пробный вектор с новыми настройками, до того как они начнут действовать
func (admin *aiAdmin) check_EmbedModel(settings ollama.Settings) error {
	vectors, err := admin.backend.Embed_WithSettings(settings, []string{texts.AIEmbedProbe})
	if err != nil || len(vectors) != 1 {
		return ollama.ErrUnavailable
	}
	if len(vectors[0]) != admin.dimensions {
		return livestore.ErrEmbedSize
	}
	return nil
}

func (admin *aiAdmin) serve_Verify(responseWriter http.ResponseWriter, request *http.Request) {
	var body struct {
		URL      string `json:"ollamaUrl"`
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
	}
	if err := read_JSONBody(responseWriter, request, &body); err != nil || !ollama.Check_URL(body.URL) {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if _, ok := admin.require_Manager(responseWriter, request); !ok {
		return
	}
	probe := ollama.Settings{Provider: body.Provider, URL: body.URL, APIKey: strings.TrimSpace(body.APIKey)}
	current := admin.backend.Read_Settings()
	if probe.APIKey == "" && probe.Provider_OrDefault() == current.Provider_OrDefault() {
		probe.APIKey = current.APIKey
	}
	version, err := admin.backend.Check_Server(probe)
	if err != nil {
		write_AIError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Version string `json:"version"`
	}{Version: version})
}

func (admin *aiAdmin) serve_Models(responseWriter http.ResponseWriter, request *http.Request) {
	if _, ok := admin.require_Manager(responseWriter, request); !ok {
		return
	}
	models, err := admin.backend.List_Models()
	if err != nil {
		write_AIError(responseWriter, err)
		return
	}
	current := admin.backend.Read_Settings()
	lines := make([]modelLine, 0, len(models))
	for _, model := range models {
		lines = append(lines, modelLine{
			ModelInfo:    model,
			UsedForChat:  check_SameModel(model.Name, current.ChatModel),
			UsedForEmbed: check_SameModel(model.Name, current.EmbedModel),
		})
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Models []modelLine `json:"models"`
	}{Models: lines})
}

func (admin *aiAdmin) serve_PullStatus(responseWriter http.ResponseWriter, request *http.Request) {
	if _, ok := admin.require_Manager(responseWriter, request); !ok {
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, admin.read_Pull())
}

func (admin *aiAdmin) serve_StartPull(responseWriter http.ResponseWriter, request *http.Request) {
	var body modelNameRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil || !ollama.Check_ModelName(body.Name) {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if _, ok := admin.require_Manager(responseWriter, request); !ok {
		return
	}
	admin.pull.mu.Lock()
	if admin.pull.running {
		admin.pull.mu.Unlock()
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorPullRunning)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	admin.pull.model = body.Name
	admin.pull.status = ""
	admin.pull.completed = 0
	admin.pull.total = 0
	admin.pull.running = true
	admin.pull.failed = false
	admin.pull.cancel = cancel
	admin.pull.mu.Unlock()
	go admin.run_Pull(ctx, body.Name)
	write_JSONResponse(responseWriter, http.StatusAccepted, admin.read_Pull())
}

func (admin *aiAdmin) run_Pull(ctx context.Context, name string) {
	err := admin.backend.Pull_Model(ctx, name, func(progress ollama.PullProgress) {
		admin.pull.mu.Lock()
		defer admin.pull.mu.Unlock()
		admin.pull.status = progress.Status
		admin.pull.completed = progress.Completed
		admin.pull.total = progress.Total
	})
	admin.pull.mu.Lock()
	defer admin.pull.mu.Unlock()
	admin.pull.running = false
	admin.pull.cancel = nil
	if err != nil {
		log.Printf("model pull %s: %v", name, err)
		admin.pull.failed = true
	}
}

func (admin *aiAdmin) serve_CancelPull(responseWriter http.ResponseWriter, request *http.Request) {
	if _, ok := admin.require_Manager(responseWriter, request); !ok {
		return
	}
	admin.pull.mu.Lock()
	if admin.pull.cancel != nil {
		admin.pull.cancel()
	}
	admin.pull.mu.Unlock()
	write_JSONResponse(responseWriter, http.StatusOK, admin.read_Pull())
}

func (admin *aiAdmin) read_Pull() pullResponse {
	admin.pull.mu.Lock()
	defer admin.pull.mu.Unlock()
	return pullResponse{
		Model:     admin.pull.model,
		Status:    admin.pull.status,
		Completed: admin.pull.completed,
		Total:     admin.pull.total,
		Running:   admin.pull.running,
		Failed:    admin.pull.failed,
	}
}

// Модель, на которой сейчас работает чат или поиск, удалить нельзя: помощник перестанет отвечать
func (admin *aiAdmin) serve_DeleteModel(responseWriter http.ResponseWriter, request *http.Request) {
	var body modelNameRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil || !ollama.Check_ModelName(body.Name) {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if _, ok := admin.require_Manager(responseWriter, request); !ok {
		return
	}
	current := admin.backend.Read_Settings()
	if check_SameModel(body.Name, current.ChatModel) || check_SameModel(body.Name, current.EmbedModel) {
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorModelInUse)
		return
	}
	if err := admin.backend.Delete_Model(body.Name); err != nil {
		write_AIError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Deleted string `json:"deleted"`
	}{Deleted: body.Name})
}

// Ollama дописывает :latest к имени без тега
func check_SameModel(left, right string) bool {
	return with_LatestTag(left) == with_LatestTag(right)
}

func with_LatestTag(name string) string {
	for index := len(name) - 1; index >= 0 && name[index] != '/'; index-- {
		if name[index] == ':' {
			return name
		}
	}
	return name + ":latest"
}

// Короткий вопрос модели: проверка, что она отвечает, и сколько думает
func (admin *aiAdmin) serve_TestModel(responseWriter http.ResponseWriter, request *http.Request) {
	var body modelNameRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil || !ollama.Check_ModelName(body.Name) {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if _, ok := admin.require_Manager(responseWriter, request); !ok {
		return
	}
	started := time.Now()
	reply, err := admin.backend.Complete_ChatWithModel(body.Name, []ollama.ChatMessage{{Role: "user", Content: texts.AITestPrompt}})
	if err != nil {
		write_AIError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Reply      string `json:"reply"`
		DurationMs int64  `json:"durationMs"`
	}{Reply: reply, DurationMs: time.Since(started).Milliseconds()})
}

func (admin *aiAdmin) serve_ReindexStatus(responseWriter http.ResponseWriter, request *http.Request) {
	token, ok := admin.require_Manager(responseWriter, request)
	if !ok {
		return
	}
	status, err := admin.reindexer.Read_ReindexStatus(request.Context(), token)
	if err != nil {
		write_AIError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_ReindexResponse(status))
}

func (admin *aiAdmin) serve_StartReindex(responseWriter http.ResponseWriter, request *http.Request) {
	token, ok := admin.require_Manager(responseWriter, request)
	if !ok {
		return
	}
	status, err := admin.reindexer.Start_Reindex(request.Context(), token)
	if err != nil {
		write_AIError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusAccepted, build_ReindexResponse(status))
}

func build_ReindexResponse(status livestore.ReindexStatus) reindexResponse {
	response := reindexResponse{Running: status.Running, Done: status.Done, Total: status.Total, Failed: status.Failed}
	if !status.FinishedAt.IsZero() {
		response.FinishedAt = status.FinishedAt.Format(timeLayout)
	}
	return response
}

func write_AIError(responseWriter http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ollama.ErrNotSupported):
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorProviderNotSupported)
	case errors.Is(err, ollama.ErrUnavailable), errors.Is(err, ollama.ErrPullFailed):
		write_ErrorResponse(responseWriter, http.StatusBadGateway, texts.ErrorOllamaUnavailable)
	case errors.Is(err, ollama.ErrModelNotFound):
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorModelNotFound)
	case errors.Is(err, livestore.ErrReindexRunning):
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorReindexRunning)
	case errors.Is(err, errStubWithoutIndex):
		write_ErrorResponse(responseWriter, http.StatusNotImplemented, texts.ErrorLiveNotReady)
	default:
		write_LiveError(responseWriter, err)
	}
}
