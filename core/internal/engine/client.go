// Package engine - клиент движка юридического ассистента (DocFinder/engine/server.py).
// Движок выполняет поиск, учитывает редакции на дату события, вызывает модель,
// проверяет числа и ссылки и формирует обоснованный отказ при недостатке данных.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("engine unavailable")

const (
	answerTimeout = 115 * time.Second
	adminTimeout  = 30 * time.Second
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// Пустой адрес - движок не настроен, ядро отвечает своим поиском
func New_Client(baseURL, token string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{baseURL: baseURL, token: strings.TrimSpace(token), http: &http.Client{Timeout: answerTimeout}}
}

type Turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Настройки модели из админ-панели. Пустые поля - значения из окружения движка
type ModelSettings struct {
	BaseURL       string   `json:"base_url,omitempty"`
	APIKey        string   `json:"api_key,omitempty"`
	LLMModel      string   `json:"llm_model,omitempty"`
	EmbModel      string   `json:"emb_model,omitempty"`
	RerankerModel string   `json:"reranker_model,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
	MaxTokens     *int     `json:"max_tokens,omitempty"`
}

type Citation struct {
	Label    string `json:"label"`
	DocID    string `json:"doc_id"`
	Document string `json:"document"`
	Path     string `json:"path"`
	Section  string `json:"section"`
	Quote    string `json:"quote"`
	Status   string `json:"status"`
	FileName string `json:"file_name"`
}

type Answer struct {
	Kind      string     `json:"kind"`
	Text      string     `json:"text"`
	Citations []Citation `json:"citations"`
	Missing   string     `json:"missing"`
	Mode      string     `json:"mode"`
	Problems  []string   `json:"problems"`
	Seconds   float64    `json:"seconds"`
}

type Document struct {
	Text     string `json:"text"`
	FileName string `json:"file_name"`
	Category string `json:"category,omitempty"`
}

func (client *Client) Answer(ctx context.Context, question string, history []Turn, settings ModelSettings) (Answer, error) {
	var answer Answer
	err := client.post(ctx, "/answer", map[string]any{"question": question, "history": history, "settings": settings}, &answer)
	if err == nil && strings.TrimSpace(answer.Text) == "" {
		return Answer{}, ErrUnavailable
	}
	return answer, err
}

// Перечитать базу: documents пустой - из каталога движка (ENGINE_KB_PATH), иначе - переданные тексты
func (client *Client) Reindex(ctx context.Context, documents []Document) error {
	ctx, cancel := context.WithTimeout(ctx, adminTimeout)
	defer cancel()
	body := map[string]any{}
	if len(documents) > 0 {
		body["documents"] = documents
	}
	return client.post(ctx, "/reindex", body, &struct{}{})
}

// Положения документа для просмотра: raw JSON движка отдаётся клиенту как есть
func (client *Client) Structure(ctx context.Context, docID string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, adminTimeout)
	defer cancel()
	var raw json.RawMessage
	err := client.get(ctx, "/documents/"+url.PathEscape(docID)+"/structure", &raw)
	return raw, err
}

func (client *Client) Documents(ctx context.Context) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, adminTimeout)
	defer cancel()
	var raw json.RawMessage
	err := client.get(ctx, "/documents", &raw)
	return raw, err
}

func (client *Client) post(ctx context.Context, path string, payload any, target any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	return client.do(request, target)
}

func (client *Client) get(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return ErrUnavailable
	}
	return client.do(request, target)
}

func (client *Client) do(request *http.Request, target any) error {
	if client == nil {
		return ErrUnavailable
	}
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return ErrUnavailable
	}
	if json.Unmarshal(body, target) != nil {
		return ErrUnavailable
	}
	return nil
}

var ErrNotFound = errors.New("engine: not found")
