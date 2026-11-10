package livestore

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

// Удаление учетной записи убирает ее токены, сессии с сообщениями, вложения, черновики
// и личные документы. Себя и главного администратора без roles.manage удалить нельзя
func TestDeleteAccountRemovesOwnedData(t *testing.T) {
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
	actorID := "user-del-actor-" + suffix
	actorToken := "token-del-actor-" + suffix
	targetID := "user-del-target-" + suffix
	sessionID := "session-del-" + suffix
	documentID := "doc-del-" + suffix
	chiefID := "user-del-chief-" + suffix
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, chiefID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM auth_tokens WHERE user_id = $1`, actorID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, actorID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM permission_events WHERE target_user_id = $1`, targetID)
	})
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (user_id, login, password_hash, display_name, access_level) VALUES ($1, $2, 'x', 'Администратор', 2)`, []any{actorID, actorID + "@corp.local"}},
		{`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, []any{actorID, texts.RoleIDAdmin}},
		{`INSERT INTO auth_tokens (token, user_id) VALUES ($1, $2)`, []any{actorToken, actorID}},
		{`INSERT INTO users (user_id, login, password_hash, display_name, access_level) VALUES ($1, $2, 'x', 'Главный', 2)`, []any{chiefID, chiefID + "@corp.local"}},
		{`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, []any{chiefID, texts.RoleIDChief}},
		{`INSERT INTO users (user_id, login, password_hash, display_name, access_level) VALUES ($1, $2, 'x', 'Сотрудник', 1)`, []any{targetID, targetID + "@corp.local"}},
		{`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, []any{targetID, texts.RoleIDUser}},
		{`INSERT INTO auth_tokens (token, user_id) VALUES ($1, $2)`, []any{"token-del-target-" + suffix, targetID}},
		{`INSERT INTO sessions (session_id, owner_user_id) VALUES ($1, $2)`, []any{sessionID, targetID}},
		{`INSERT INTO session_messages (session_id, position, body) VALUES ($1, 0, '{}'::jsonb)`, []any{sessionID}},
		{`INSERT INTO session_attachments (attachment_id, session_id, owner_user_id, file_name, plain_text, characters)
			VALUES ($1, $2, $3, 'draft.txt', 'Текст', 5)`, []any{"att-del-" + suffix, sessionID, targetID}},
		{`INSERT INTO draft_files (draft_id, owner_user_id, file_name, content_type, body) VALUES ($1, $2, 'blank.docx', 'application/octet-stream', $3)`, []any{"draft-del-" + suffix, targetID, []byte("docx")}},
		{`INSERT INTO documents (document_id, title, edition, access_level, scope, owner_id, file_name, content_type, body)
			VALUES ($1, 'Личный', '', 1, $2, $3, 'mine.txt', 'text/plain', $4)`, []any{documentID, texts.ScopePersonal, targetID, []byte("text")}},
	}
	for _, statement := range statements {
		if _, err := store.pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Delete_Account(ctx, actorToken, actorID); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("self delete %v", err)
	}
	if err := store.Delete_Account(ctx, actorToken, chiefID); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("admin deleted the chief %v", err)
	}
	if err := store.Delete_Account(ctx, actorToken, "user-missing-"+suffix); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user %v", err)
	}
	if err := store.Delete_Account(ctx, actorToken, targetID); err != nil {
		t.Fatal(err)
	}
	leftovers := map[string]string{
		"users":               `SELECT count(*) FROM users WHERE user_id = $1`,
		"auth_tokens":         `SELECT count(*) FROM auth_tokens WHERE user_id = $1`,
		"sessions":            `SELECT count(*) FROM sessions WHERE owner_user_id = $1`,
		"session_attachments": `SELECT count(*) FROM session_attachments WHERE owner_user_id = $1`,
		"draft_files":         `SELECT count(*) FROM draft_files WHERE owner_user_id = $1`,
		"documents":           `SELECT count(*) FROM documents WHERE owner_id = $1`,
		"user_roles":          `SELECT count(*) FROM user_roles WHERE user_id = $1`,
	}
	for table, query := range leftovers {
		var count int
		if err := store.pool.QueryRow(ctx, query, targetID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s left %d %v", table, count, err)
		}
	}
	var messages int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM session_messages WHERE session_id = $1`, sessionID).Scan(&messages); err != nil || messages != 0 {
		t.Fatalf("messages left %d %v", messages, err)
	}
	var events int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM permission_events WHERE action = $1 AND target_user_id = $2`, texts.PermissionActionDeleteUser, targetID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("delete event %d %v", events, err)
	}
}
