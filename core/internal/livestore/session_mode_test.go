package livestore

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

// Режим чата хранится в сессии: новая и старая сессия - «Чтение», сохраненный режим читается обратно
func TestSessionModeIsStored(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is empty")
	}
	store, err := Open_LiveStore(databaseURL, 768, nil, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "user-mode-" + suffix
	sessionID := "session-mode-" + suffix
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM sessions WHERE session_id = $1`, sessionID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
	})
	if _, err := store.pool.Exec(ctx, `INSERT INTO users (user_id, login, password_hash, display_name, access_level) VALUES ($1, $2, 'x', 'Режим', 1)`, userID, userID+"@corp.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO sessions (session_id, owner_user_id) VALUES ($1, $2)`, sessionID, userID); err != nil {
		t.Fatal(err)
	}
	if mode, err := store.Read_SessionMode(ctx, sessionID); err != nil || mode != texts.ChatModeRead {
		t.Fatalf("default mode %q %v", mode, err)
	}
	if err := store.Save_SessionMode(ctx, sessionID, texts.ChatModePlan); err != nil {
		t.Fatal(err)
	}
	if mode, err := store.Read_SessionMode(ctx, sessionID); err != nil || mode != texts.ChatModePlan {
		t.Fatalf("saved mode %q %v", mode, err)
	}
	if _, err := store.Read_SessionMode(ctx, "session-missing-"+suffix); err != ErrNotFound {
		t.Fatalf("missing session %v", err)
	}
}
