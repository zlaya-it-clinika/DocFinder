package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Управление моделями из админ-панели: версия сервера, список, скачивание и удаление, как в open-webui

var (
	ErrModelNotFound = errors.New("model not found")
	ErrPullFailed    = errors.New("pull failed")
	// Скачивание и удаление моделей есть только у Ollama
	ErrNotSupported = errors.New("not supported by provider")
)

const adminTimeout = 15 * time.Second

// Имя модели из библиотеки Ollama или Hugging Face: qwen2.5:7b, hf.co/user/repo:Q4_K_M
var modelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,199}$`)

func Check_ModelName(name string) bool {
	return modelNamePattern.MatchString(strings.TrimSpace(name))
}

type ModelInfo struct {
	Name          string    `json:"name"`
	Size          int64     `json:"size"`
	ModifiedAt    time.Time `json:"modifiedAt"`
	Family        string    `json:"family"`
	ParameterSize string    `json:"parameterSize"`
	Quantization  string    `json:"quantization"`
	// Модель сейчас загружена в память Ollama
	Loaded bool `json:"loaded"`
}

// Ход скачивания модели: строка статуса Ollama и байты текущего слоя
type PullProgress struct {
	Status    string
	Completed int64
	Total     int64
}

// Проверка сервера по адресу из формы, до сохранения настроек. Для Ollama - версия,
// для OpenAI-совместимого API - число доступных моделей по /models с переданным ключом
func (client *Client) Check_Server(settings Settings) (string, error) {
	settings.URL = Normalize_URL(settings.URL)
	if settings.Provider_OrDefault() == ProviderOpenAI {
		models, err := client.openai_Models(settings)
		if err != nil {
			return "", err
		}
		return "OpenAI-compatible API, моделей: " + strconv.Itoa(len(models)), nil
	}
	return client.Check_Version(settings.URL)
}

// Версия сервера по адресу из формы, до сохранения настроек
func (client *Client) Check_Version(baseURL string) (string, error) {
	var parsed struct {
		Version string `json:"version"`
	}
	if err := client.get_JSON(Normalize_URL(baseURL)+"/api/version", &parsed); err != nil {
		return "", err
	}
	if parsed.Version == "" {
		return "", ErrUnavailable
	}
	return parsed.Version, nil
}

func (client *Client) List_Models() ([]ModelInfo, error) {
	settings := client.Read_Settings()
	if settings.Provider_OrDefault() == ProviderOpenAI {
		return client.openai_Models(settings)
	}
	baseURL := settings.URL
	var tags struct {
		Models []struct {
			Name       string    `json:"name"`
			Size       int64     `json:"size"`
			ModifiedAt time.Time `json:"modified_at"`
			Details    struct {
				Family            string `json:"family"`
				ParameterSize     string `json:"parameter_size"`
				QuantizationLevel string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := client.get_JSON(baseURL+"/api/tags", &tags); err != nil {
		return nil, err
	}
	// Список загруженных в память не обязателен: старые версии Ollama /api/ps не знают
	loaded := map[string]bool{}
	var running struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if client.get_JSON(baseURL+"/api/ps", &running) == nil {
		for _, model := range running.Models {
			loaded[model.Name] = true
		}
	}
	models := make([]ModelInfo, 0, len(tags.Models))
	for _, model := range tags.Models {
		models = append(models, ModelInfo{
			Name:          model.Name,
			Size:          model.Size,
			ModifiedAt:    model.ModifiedAt,
			Family:        model.Details.Family,
			ParameterSize: model.Details.ParameterSize,
			Quantization:  model.Details.QuantizationLevel,
			Loaded:        loaded[model.Name],
		})
	}
	return models, nil
}

// Скачивание с потоком статусов. progress вызывается на каждую строку ответа Ollama
func (client *Client) Pull_Model(ctx context.Context, name string, progress func(PullProgress)) error {
	if client.Read_Settings().Provider_OrDefault() != ProviderOllama {
		return ErrNotSupported
	}
	payload, err := json.Marshal(struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}{Model: strings.TrimSpace(name), Stream: true})
	if err != nil {
		return ErrPullFailed
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.Read_Settings().URL+"/api/pull", bytes.NewReader(payload))
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.pullClient.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return ErrPullFailed
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	succeeded := false
	for scanner.Scan() {
		var line struct {
			Status    string `json:"status"`
			Error     string `json:"error"`
			Completed int64  `json:"completed"`
			Total     int64  `json:"total"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		if line.Error != "" {
			return ErrPullFailed
		}
		progress(PullProgress{Status: line.Status, Completed: line.Completed, Total: line.Total})
		if line.Status == "success" {
			succeeded = true
		}
	}
	if err := scanner.Err(); err != nil || !succeeded {
		return ErrPullFailed
	}
	return nil
}

func (client *Client) Delete_Model(name string) error {
	if client.Read_Settings().Provider_OrDefault() != ProviderOllama {
		return ErrNotSupported
	}
	payload, err := json.Marshal(struct {
		Model string `json:"model"`
	}{Model: strings.TrimSpace(name)})
	if err != nil {
		return ErrUnavailable
	}
	request, err := http.NewRequest(http.MethodDelete, client.Read_Settings().URL+"/api/delete", bytes.NewReader(payload))
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.adminClient.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	switch {
	case response.StatusCode == http.StatusNotFound:
		return ErrModelNotFound
	case response.StatusCode >= 300:
		return ErrUnavailable
	}
	return nil
}

func (client *Client) get_JSON(address string, target any) error {
	if address == "" || !strings.HasPrefix(address, "http") {
		return ErrUnavailable
	}
	response, err := client.adminClient.Get(address)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	if json.Unmarshal(body, target) != nil {
		return ErrUnavailable
	}
	return nil
}
