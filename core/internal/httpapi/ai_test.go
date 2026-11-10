package httpapi

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

// Права и проверка запросов не требуют Ollama: адрес из теста никуда не ведёт
func TestAIRoutesAccessAndValidation(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub, OllamaURL: "http://127.0.0.1:1", OllamaChatModel: "chat", OllamaEmbedModel: "embed", OllamaEmbedDimensions: 768})
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)

	for _, path := range []string{"/ai/settings", "/ai/models", "/ai/models/pull"} {
		if code := doJSON(t, handler, http.MethodGet, path, employeeToken, nil).Code; code != http.StatusForbidden {
			t.Fatalf("employee %s: %d", path, code)
		}
	}
	settings := decodeJSON[aiSettingsResponse](t, doJSON(t, handler, http.MethodGet, "/ai/settings", chiefToken, nil))
	if settings.URL != "http://127.0.0.1:1" || settings.ChatModel != "chat" || settings.EmbedDimensions != 768 || settings.Limits.NumCtx.Max == 0 {
		t.Fatalf("settings %+v", settings)
	}

	if doJSON(t, handler, http.MethodPost, "/ai/verify", chiefToken, map[string]string{"ollamaUrl": "ftp://host"}).Code != http.StatusBadRequest {
		t.Fatal("ftp url accepted")
	}
	offline := doJSON(t, handler, http.MethodPost, "/ai/verify", chiefToken, map[string]string{"ollamaUrl": "http://127.0.0.1:1"})
	if offline.Code != http.StatusBadGateway || !strings.Contains(offline.Body.String(), texts.ErrorOllamaUnavailable) {
		t.Fatalf("offline verify %d %s", offline.Code, offline.Body.String())
	}
	for _, body := range []map[string]any{
		{"ollamaUrl": "не адрес", "chatModel": "chat", "embedModel": "embed"},
		{"ollamaUrl": "http://127.0.0.1:1", "chatModel": "", "embedModel": "embed"},
		{"ollamaUrl": "http://127.0.0.1:1", "chatModel": "chat", "embedModel": "embed", "temperature": 5},
		{"ollamaUrl": "http://127.0.0.1:1", "chatModel": "chat", "embedModel": "embed", "numCtx": 10},
	} {
		if code := doJSON(t, handler, http.MethodPost, "/ai/settings", chiefToken, body).Code; code != http.StatusBadRequest {
			t.Fatalf("bad settings %v: %d", body, code)
		}
	}
	// Параметры сохраняются без проверки Ollama, пока модель эмбеддингов и адрес прежние
	saved := doJSON(t, handler, http.MethodPost, "/ai/settings", chiefToken, map[string]any{
		"ollamaUrl": "http://127.0.0.1:1/", "chatModel": "other-chat", "embedModel": "embed", "temperature": 0.2,
	})
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `"temperature":0.2`) {
		t.Fatalf("save %d %s", saved.Code, saved.Body.String())
	}
	// Новую модель эмбеддингов без ответа Ollama не принять: размер вектора не проверить
	if code := doJSON(t, handler, http.MethodPost, "/ai/settings", chiefToken, map[string]any{
		"ollamaUrl": "http://127.0.0.1:1", "chatModel": "other-chat", "embedModel": "other-embed",
	}).Code; code != http.StatusBadGateway {
		t.Fatalf("embed change without ollama: %d", code)
	}

	if doJSON(t, handler, http.MethodPost, "/ai/models/pull", chiefToken, map[string]string{"name": "bad name;rm"}).Code != http.StatusBadRequest {
		t.Fatal("bad model name accepted")
	}
	inUse := doJSON(t, handler, http.MethodPost, "/ai/models/delete", chiefToken, map[string]string{"name": "other-chat:latest"})
	if inUse.Code != http.StatusConflict || !strings.Contains(inUse.Body.String(), texts.ErrorModelInUse) {
		t.Fatalf("delete in use %d %s", inUse.Code, inUse.Body.String())
	}
	reindex := doJSON(t, handler, http.MethodPost, "/ai/reindex", chiefToken, nil)
	if reindex.Code != http.StatusNotImplemented || !strings.Contains(reindex.Body.String(), texts.ErrorLiveNotReady) {
		t.Fatalf("stub reindex %d %s", reindex.Code, reindex.Body.String())
	}
}

// Настоящая Ollama: OLLAMA_TEST_URL и уже скачанная модель OLLAMA_TEST_CHAT_MODEL
func TestAIRoutesWithOllama(t *testing.T) {
	ollamaURL := os.Getenv("OLLAMA_TEST_URL")
	chatModel := os.Getenv("OLLAMA_TEST_CHAT_MODEL")
	if ollamaURL == "" || chatModel == "" {
		t.Skip("OLLAMA_TEST_URL or OLLAMA_TEST_CHAT_MODEL is empty")
	}
	handler := must_Handler(t, config.Config{Mode: config.ModeStub, OllamaURL: ollamaURL, OllamaChatModel: chatModel, OllamaEmbedModel: chatModel, OllamaEmbedDimensions: 768})
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)

	verify := doJSON(t, handler, http.MethodPost, "/ai/verify", chiefToken, map[string]string{"ollamaUrl": ollamaURL})
	if verify.Code != http.StatusOK || !strings.Contains(verify.Body.String(), `"version"`) {
		t.Fatalf("verify %d %s", verify.Code, verify.Body.String())
	}
	models := doJSON(t, handler, http.MethodGet, "/ai/models", chiefToken, nil)
	if models.Code != http.StatusOK || !strings.Contains(models.Body.String(), `"usedForChat":true`) {
		t.Fatalf("models %d %s", models.Code, models.Body.String())
	}
	tested := doJSON(t, handler, http.MethodPost, "/ai/models/test", chiefToken, map[string]string{"name": chatModel})
	if tested.Code != http.StatusOK || !strings.Contains(tested.Body.String(), "durationMs") {
		t.Fatalf("test %d %s", tested.Code, tested.Body.String())
	}
	if doJSON(t, handler, http.MethodPost, "/ai/models/delete", chiefToken, map[string]string{"name": "docfinder-missing-model:latest"}).Code != http.StatusNotFound {
		t.Fatal("missing model deleted")
	}
}

func TestModelNamesWithLatestTag(t *testing.T) {
	if !check_SameModel("vikhr-nemo", "vikhr-nemo:latest") || check_SameModel("qwen2.5:7b", "qwen2.5:14b") {
		t.Fatal("tag comparison")
	}
	if with_LatestTag("hf.co/user/repo") != "hf.co/user/repo:latest" || with_LatestTag("hf.co/user/repo:Q4_K_M") != "hf.co/user/repo:Q4_K_M" {
		t.Fatal("hf names")
	}
}
