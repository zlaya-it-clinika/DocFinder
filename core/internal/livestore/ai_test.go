package livestore

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

func TestAISettingsAndPermission(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is empty")
	}
	// Переиндексация - с настоящей Ollama, если она задана: OLLAMA_TEST_URL и модель эмбеддингов на 768
	ollamaURL, embedModel := os.Getenv("OLLAMA_TEST_URL"), os.Getenv("OLLAMA_TEST_EMBED_MODEL")
	var embedder Embedder
	if ollamaURL != "" && embedModel != "" {
		embedder = ollama.New_Client(ollamaURL, embedModel, "")
	}
	store, err := Open_LiveStore(databaseURL, 768, embedder, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	ctx := context.Background()

	has_AdminFlag := func() bool {
		var found bool
		if err := store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM role_permissions WHERE role_id = $1 AND permission = $2)`, texts.RoleIDAdmin, texts.PermissionAIManage).Scan(&found); err != nil {
			t.Fatal(err)
		}
		return found
	}
	if !has_AdminFlag() {
		t.Fatal("admin role without ai.manage")
	}
	// Снятый в конструкторе флаг не возвращается при следующем запуске
	if _, err := store.pool.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1 AND permission = $2`, texts.RoleIDAdmin, texts.PermissionAIManage); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING`, texts.RoleIDAdmin, texts.PermissionAIManage)
	})
	if err := store.grant_AIPermission(ctx); err != nil {
		t.Fatal(err)
	}
	if has_AdminFlag() {
		t.Fatal("ai.manage granted twice")
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	chiefID, employeeID := "user-ai-chief-"+suffix, "user-ai-employee-"+suffix
	chiefToken, employeeToken := "token-ai-chief-"+suffix, "token-ai-employee-"+suffix
	for _, user := range []struct{ id, token, role string }{{chiefID, chiefToken, texts.RoleIDChief}, {employeeID, employeeToken, texts.RoleIDUser}} {
		if _, err := store.pool.Exec(ctx, `
			INSERT INTO users (user_id, login, password_hash, display_name, access_level)
			VALUES ($1, $2, 'x', 'Тест', 1)`, user.id, user.id+"@test.local"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, user.id, user.role); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO auth_tokens (token, user_id) VALUES ($1, $2)`, user.token, user.id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		background := context.Background()
		_, _ = store.pool.Exec(background, `DELETE FROM auth_tokens WHERE token IN ($1, $2)`, chiefToken, employeeToken)
		_, _ = store.pool.Exec(background, `DELETE FROM user_roles WHERE user_id IN ($1, $2)`, chiefID, employeeID)
		_, _ = store.pool.Exec(background, `DELETE FROM users WHERE user_id IN ($1, $2)`, chiefID, employeeID)
		_, _ = store.pool.Exec(background, `DELETE FROM app_settings WHERE key = $1`, settingsKeyAI)
	})

	if _, found, err := store.Read_SavedAISettings(ctx); err != nil || found {
		t.Fatalf("settings before save: %v %v", found, err)
	}
	temperature := 0.25
	settings := ollama.Settings{URL: "http://ollama:11434", ChatModel: "chat", EmbedModel: "embed", Temperature: &temperature}
	if err := store.Save_AISettings(ctx, employeeToken, settings); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("employee saved ai settings: %v", err)
	}
	if err := store.Save_AISettings(ctx, chiefToken, settings); err != nil {
		t.Fatal(err)
	}
	saved, found, err := store.Read_SavedAISettings(ctx)
	if err != nil || !found || saved.ChatModel != "chat" || saved.Temperature == nil || *saved.Temperature != 0.25 || saved.TopK != nil {
		t.Fatalf("saved %+v %v %v", saved, found, err)
	}

	if _, err := store.Start_Reindex(ctx, employeeToken); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("employee reindex: %v", err)
	}
	if embedder == nil {
		return
	}
	if _, err := store.Start_Reindex(ctx, chiefToken); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		status, err := store.Read_ReindexStatus(ctx, chiefToken)
		if err != nil {
			t.Fatal(err)
		}
		if !status.Running {
			if status.Failed || status.Done != status.Total || status.FinishedAt.IsZero() {
				t.Fatalf("reindex status %+v", status)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("reindex did not finish")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
