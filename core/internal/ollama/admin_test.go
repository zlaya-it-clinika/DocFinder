package ollama

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestSettingsNormalizeAndOptions(t *testing.T) {
	topP := 1.5
	cases := []struct {
		settings Settings
		valid    bool
	}{
		{Settings{URL: " http://localhost:11434/ ", ChatModel: "a", EmbedModel: "b"}, true},
		{Settings{URL: "localhost:11434", ChatModel: "a", EmbedModel: "b"}, false},
		{Settings{URL: "http://user:pass@host", ChatModel: "a", EmbedModel: "b"}, false},
		{Settings{URL: "http://host", ChatModel: "a", EmbedModel: "b", TopP: &topP}, false},
	}
	for _, item := range cases {
		normalized, valid := item.settings.Normalize()
		if valid != item.valid {
			t.Fatalf("%+v: %v", item.settings, valid)
		}
		if valid && normalized.URL != "http://localhost:11434" {
			t.Fatalf("url %q", normalized.URL)
		}
	}
	if !Check_ModelName("hf.co/Vikhrmodels/Vikhr-Nemo-12B-Instruct-R-21-09-24-GGUF:Q4_K_M") || Check_ModelName("a b") || Check_ModelName("") {
		t.Fatal("model names")
	}

	// Без параметров генерации поле options не отправляется
	if (Settings{}).chat_Options() != nil {
		t.Fatal("empty options sent")
	}
	temperature, numCtx := 0.3, 8192
	options := Settings{Temperature: &temperature, NumCtx: &numCtx}.chat_Options()
	if options["temperature"] != 0.3 || options["num_ctx"] != 8192 || options["top_k"] != nil {
		t.Fatalf("options %v", options)
	}
}

func TestClientUnreachable(t *testing.T) {
	client := New_Client("http://127.0.0.1:1", "embed", "chat")
	if _, err := client.Check_Version("http://127.0.0.1:1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("version %v", err)
	}
	if _, err := client.List_Models(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("models %v", err)
	}
}

// Настоящая Ollama: OLLAMA_TEST_URL. OLLAMA_TEST_PULL_MODEL - небольшая модель для проверки скачивания
func TestClientWithOllama(t *testing.T) {
	ollamaURL := os.Getenv("OLLAMA_TEST_URL")
	if ollamaURL == "" {
		t.Skip("OLLAMA_TEST_URL is empty")
	}
	client := New_Client(ollamaURL, "", "")
	if version, err := client.Check_Version(ollamaURL); err != nil || version == "" {
		t.Fatalf("version %q %v", version, err)
	}
	if _, err := client.List_Models(); err != nil {
		t.Fatalf("models %v", err)
	}
	if err := client.Delete_Model("docfinder-missing-model:latest"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("delete missing %v", err)
	}
	pullModel := os.Getenv("OLLAMA_TEST_PULL_MODEL")
	if pullModel == "" {
		return
	}
	succeeded := false
	if err := client.Pull_Model(context.Background(), pullModel, func(progress PullProgress) {
		succeeded = succeeded || progress.Status == "success"
	}); err != nil || !succeeded {
		t.Fatalf("pull %v", err)
	}
}
