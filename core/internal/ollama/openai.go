package ollama

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
)

// OpenAI-совместимый API: адрес вида https://host/v1, ключ в заголовке Authorization: Bearer.
// Используются только /chat/completions, /embeddings и /models - без tools и response_format,
// чтобы работать с любой моделью за прокси: OpenRouter, vLLM или Ollama /v1.

func openai_Request(settings Settings, method, path string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, ErrUnavailable
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, settings.URL+path, body)
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	if settings.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+settings.APIKey)
	}
	return request, nil
}

func openai_Do(client *http.Client, request *http.Request, limit int64, target any) error {
	response, err := client.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	if json.Unmarshal(raw, target) != nil {
		return ErrUnavailable
	}
	return nil
}

func (client *Client) openai_Chat(settings Settings, messages []ChatMessage) (string, error) {
	payload := map[string]any{"model": settings.ChatModel, "messages": messages}
	if settings.Temperature != nil {
		payload["temperature"] = *settings.Temperature
	}
	if settings.TopP != nil {
		payload["top_p"] = *settings.TopP
	}
	if settings.MaxTokens != nil {
		payload["max_tokens"] = *settings.MaxTokens
	}
	request, err := openai_Request(settings, http.MethodPost, "/chat/completions", payload)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := openai_Do(client.chatClient, request, 4<<20, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", ErrUnavailable
	}
	return parsed.Choices[0].Message.Content, nil
}

func (client *Client) openai_Embed(settings Settings, model string, inputs []string) ([][]float32, error) {
	request, err := openai_Request(settings, http.MethodPost, "/embeddings", map[string]any{
		"model": model, "input": inputs, "encoding_format": "float",
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := openai_Do(client.embedClient, request, 32<<20, &parsed); err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(inputs))
	for position, item := range parsed.Data {
		index := item.Index
		if index == 0 && position > 0 {
			index = position
		}
		if index < 0 || index >= len(inputs) || len(item.Embedding) == 0 {
			return nil, ErrUnavailable
		}
		vectors[index] = normalize_Vector(item.Embedding)
	}
	for _, vector := range vectors {
		if vector == nil {
			return nil, ErrUnavailable
		}
	}
	return vectors, nil
}

// Векторы bge-m3 через прокси могут прийти ненормированными, а pgvector сравнивает косинусом:
// нормируем, чтобы одинаково считалась близость при любом провайдере
func normalize_Vector(vector []float32) []float32 {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	if sum == 0 {
		return vector
	}
	norm := float32(math.Sqrt(sum))
	result := make([]float32, len(vector))
	for index, value := range vector {
		result[index] = value / norm
	}
	return result
}

// Список моделей OpenAI-совместимого API (/models). Проверка доступа - тот же запрос
func (client *Client) openai_Models(settings Settings) ([]ModelInfo, error) {
	request, err := openai_Request(settings, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := openai_Do(client.adminClient, request, 8<<20, &parsed); err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.ID != "" {
			models = append(models, ModelInfo{Name: item.ID})
		}
	}
	return models, nil
}
