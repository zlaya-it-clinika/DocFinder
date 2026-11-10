package ollama

import (
	"net/url"
	"strings"
)

// Провайдер модели: Ollama (родной API /api/chat, /api/embed, управление моделями)
// или любой OpenAI-совместимый API со своим адресом (/chat/completions, /embeddings, /rerank)
const (
	ProviderOllama = "ollama"
	ProviderOpenAI = "openai"
)

// Настройки модели, как разделы «Подключения» и «Модели» в open-webui.
// Пустой указатель параметра генерации - значение по умолчанию модели
type Settings struct {
	Provider    string   `json:"provider"`
	URL         string   `json:"ollamaUrl"`
	ChatModel   string   `json:"chatModel"`
	EmbedModel  string   `json:"embedModel"`
	Temperature *float64 `json:"temperature"`
	TopK        *int     `json:"topK"`
	TopP        *float64 `json:"topP"`
	NumCtx      *int     `json:"numCtx"`
	// Только для OpenAI-совместимого API. Ключ не отдаётся клиентам: в ответах только признак APIKeySet
	APIKey      string `json:"apiKey,omitempty"`
	RerankModel string `json:"rerankModel"`
	MaxTokens   *int   `json:"maxTokens"`
}

// Провайдер по умолчанию - Ollama: так читаются настройки, сохранённые до появления выбора
func (settings Settings) Provider_OrDefault() string {
	if strings.EqualFold(strings.TrimSpace(settings.Provider), ProviderOpenAI) {
		return ProviderOpenAI
	}
	return ProviderOllama
}

// Настройки без ключа для ответа клиенту
func (settings Settings) Without_Secret() Settings {
	settings.APIKey = ""
	return settings
}

// Границы параметров генерации, за ними модели ведут себя непредсказуемо
const (
	maxTemperature = 2.0
	maxTopK        = 1000
	minNumCtx      = 512
	maxNumCtx      = 131072
	minMaxTokens   = 64
	maxMaxTokens   = 32768
)

// Границы одного параметра для формы админ-панели, чтобы сайт не держал свою копию чисел
type ParamLimit struct {
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Integer bool    `json:"integer"`
}

type ParamLimits struct {
	Temperature ParamLimit `json:"temperature"`
	TopK        ParamLimit `json:"topK"`
	TopP        ParamLimit `json:"topP"`
	NumCtx      ParamLimit `json:"numCtx"`
	MaxTokens   ParamLimit `json:"maxTokens"`
}

// Top P строго больше нуля: ноль Ollama не принимает, поэтому нижняя граница - минимальный шаг формы
const minTopP = 0.01

func Read_ParamLimits() ParamLimits {
	return ParamLimits{
		Temperature: ParamLimit{Min: 0, Max: maxTemperature},
		TopK:        ParamLimit{Min: 1, Max: maxTopK, Integer: true},
		TopP:        ParamLimit{Min: minTopP, Max: 1},
		NumCtx:      ParamLimit{Min: minNumCtx, Max: maxNumCtx, Integer: true},
		MaxTokens:   ParamLimit{Min: minMaxTokens, Max: maxMaxTokens, Integer: true},
	}
}

func Normalize_URL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// Адрес Ollama: http или https с хостом, без логина и пароля в адресе
func Check_URL(raw string) bool {
	parsed, err := url.Parse(Normalize_URL(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// Приводит настройки к виду для сохранения. false - значение вне допустимых границ
func (settings Settings) Normalize() (Settings, bool) {
	settings.Provider = settings.Provider_OrDefault()
	settings.URL = Normalize_URL(settings.URL)
	settings.ChatModel = strings.TrimSpace(settings.ChatModel)
	settings.EmbedModel = strings.TrimSpace(settings.EmbedModel)
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	settings.RerankModel = strings.TrimSpace(settings.RerankModel)
	if settings.Provider == ProviderOllama {
		// У Ollama нет ключа и реранкера, а параметры top_k и num_ctx есть
		settings.APIKey = ""
		settings.RerankModel = ""
	} else {
		settings.TopK = nil
		settings.NumCtx = nil
	}
	if settings.MaxTokens != nil && (*settings.MaxTokens < minMaxTokens || *settings.MaxTokens > maxMaxTokens) {
		return settings, false
	}
	if !Check_URL(settings.URL) || settings.ChatModel == "" || settings.EmbedModel == "" {
		return settings, false
	}
	if settings.Temperature != nil && (*settings.Temperature < 0 || *settings.Temperature > maxTemperature) {
		return settings, false
	}
	if settings.TopK != nil && (*settings.TopK < 1 || *settings.TopK > maxTopK) {
		return settings, false
	}
	if settings.TopP != nil && (*settings.TopP < minTopP || *settings.TopP > 1) {
		return settings, false
	}
	if settings.NumCtx != nil && (*settings.NumCtx < minNumCtx || *settings.NumCtx > maxNumCtx) {
		return settings, false
	}
	return settings, true
}

// Параметры генерации для поля options запроса /api/chat. nil - запрос без options
func (settings Settings) chat_Options() map[string]any {
	options := map[string]any{}
	if settings.Temperature != nil {
		options["temperature"] = *settings.Temperature
	}
	if settings.TopK != nil {
		options["top_k"] = *settings.TopK
	}
	if settings.TopP != nil {
		options["top_p"] = *settings.TopP
	}
	if settings.NumCtx != nil {
		options["num_ctx"] = *settings.NumCtx
	}
	if len(options) == 0 {
		return nil
	}
	return options
}
