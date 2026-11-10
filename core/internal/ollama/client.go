package ollama

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("embed unavailable")

const (
	embedTimeout = 60 * time.Second
	chatTimeout  = 120 * time.Second
)

// Клиент Ollama. Адрес, модели и параметры генерации меняются на лету из админ-панели
type Client struct {
	mu          sync.RWMutex
	settings    Settings
	embedClient *http.Client
	chatClient  *http.Client
	adminClient *http.Client
	pullClient  *http.Client
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func New_Client(baseURL, embedModel, chatModel string) *Client {
	return &Client{
		settings: Settings{
			URL:        Normalize_URL(baseURL),
			EmbedModel: strings.TrimSpace(embedModel),
			ChatModel:  strings.TrimSpace(chatModel),
		},
		embedClient: &http.Client{Timeout: embedTimeout},
		chatClient:  &http.Client{Timeout: chatTimeout},
		adminClient: &http.Client{Timeout: adminTimeout},
		// Скачивание модели идёт минутами, его ограничивает только отмена
		pullClient: &http.Client{},
	}
}

func (client *Client) Read_Settings() Settings {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.settings
}

func (client *Client) Apply_Settings(settings Settings) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.settings = settings
}

func (client *Client) Embed_Texts(inputs []string) ([][]float32, error) {
	if client == nil {
		return nil, ErrUnavailable
	}
	settings := client.Read_Settings()
	if settings.Provider_OrDefault() == ProviderOpenAI {
		if len(inputs) == 0 {
			return nil, nil
		}
		if settings.URL == "" || settings.EmbedModel == "" {
			return nil, ErrUnavailable
		}
		return client.openai_Embed(settings, settings.EmbedModel, inputs)
	}
	return client.embed_With(settings.URL, settings.EmbedModel, inputs)
}

// Эмбеддинги по другому адресу и другой моделью: проверка размера вектора до сохранения настроек.
// Провайдер и ключ - из текущих настроек; для проверки новых настроек целиком - Embed_WithSettings
func (client *Client) Embed_WithModel(baseURL, model string, inputs []string) ([][]float32, error) {
	if client == nil {
		return nil, ErrUnavailable
	}
	settings := client.Read_Settings()
	settings.URL = Normalize_URL(baseURL)
	settings.EmbedModel = strings.TrimSpace(model)
	return client.Embed_WithSettings(settings, inputs)
}

func (client *Client) Embed_WithSettings(settings Settings, inputs []string) ([][]float32, error) {
	if client == nil {
		return nil, ErrUnavailable
	}
	if settings.Provider_OrDefault() == ProviderOpenAI {
		if settings.URL == "" || settings.EmbedModel == "" {
			return nil, ErrUnavailable
		}
		return client.openai_Embed(settings, settings.EmbedModel, inputs)
	}
	return client.embed_With(Normalize_URL(settings.URL), strings.TrimSpace(settings.EmbedModel), inputs)
}

func (client *Client) embed_With(baseURL, model string, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if baseURL == "" || model == "" {
		return nil, ErrUnavailable
	}
	payload, err := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{Model: model, Input: inputs})
	if err != nil {
		return nil, ErrUnavailable
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.embedClient.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil || response.StatusCode >= 300 {
		return nil, ErrUnavailable
	}
	var parsed struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, ErrUnavailable
	}
	if len(parsed.Embeddings) != len(inputs) {
		return nil, ErrUnavailable
	}
	return parsed.Embeddings, nil
}

func (client *Client) Complete_Chat(messages []ChatMessage) (string, error) {
	if client == nil {
		return "", ErrUnavailable
	}
	return client.chat_With(client.Read_Settings(), messages)
}

// Ответ выбранной модели с текущими параметрами генерации: кнопка «Проверить» у модели
func (client *Client) Complete_ChatWithModel(model string, messages []ChatMessage) (string, error) {
	if client == nil {
		return "", ErrUnavailable
	}
	settings := client.Read_Settings()
	settings.ChatModel = strings.TrimSpace(model)
	return client.chat_With(settings, messages)
}

func (client *Client) chat_With(settings Settings, messages []ChatMessage) (string, error) {
	if settings.URL == "" || settings.ChatModel == "" || len(messages) == 0 {
		return "", ErrUnavailable
	}
	if settings.Provider_OrDefault() == ProviderOpenAI {
		return client.openai_Chat(settings, messages)
	}
	payload, err := json.Marshal(struct {
		Model    string         `json:"model"`
		Messages []ChatMessage  `json:"messages"`
		Stream   bool           `json:"stream"`
		Options  map[string]any `json:"options,omitempty"`
	}{Model: settings.ChatModel, Messages: messages, Stream: false, Options: settings.chat_Options()})
	if err != nil {
		return "", ErrUnavailable
	}
	request, err := http.NewRequest(http.MethodPost, settings.URL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.chatClient.Do(request)
	if err != nil {
		return "", ErrUnavailable
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode >= 300 {
		return "", ErrUnavailable
	}
	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", ErrUnavailable
	}
	if strings.TrimSpace(parsed.Message.Content) == "" {
		return "", ErrUnavailable
	}
	return parsed.Message.Content, nil
}
