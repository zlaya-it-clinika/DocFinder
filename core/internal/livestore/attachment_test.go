package livestore

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"docfinder/core/internal/auth"
)

func TestSessionDeleteRemovesAttachmentAndDraft(t *testing.T) {
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
	userID := "user-att-" + suffix
	sessionID := "session-att-" + suffix
	attachmentID := "att-probe-" + suffix
	draftID := "draft-probe-" + suffix
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM sessions WHERE session_id = $1`, sessionID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
	})
	_, err = store.pool.Exec(ctx, `
		INSERT INTO users (user_id, login, password_hash, display_name, access_level)
		VALUES ($1, $2, 'x', 'Черновик', 1)`, userID, userID+"@corp.local")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO sessions (session_id, owner_user_id) VALUES ($1, $2)`, sessionID, userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO session_attachments (attachment_id, session_id, owner_user_id, file_name, plain_text, characters)
		VALUES ($1, $2, $3, 'draft.txt', 'Текст черновика', 15)`, attachmentID, sessionID, userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO draft_files (draft_id, owner_user_id, file_name, content_type, body, session_id)
		VALUES ($1, $2, 'draft.docx', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', $3, $4)`,
		draftID, userID, []byte("docx"), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	var attachments int
	var drafts int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM session_attachments WHERE session_id = $1`, sessionID).Scan(&attachments); err != nil || attachments != 1 {
		t.Fatalf("attachments before %d %v", attachments, err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM draft_files WHERE draft_id = $1`, draftID).Scan(&drafts); err != nil || drafts != 1 {
		t.Fatalf("drafts before %d %v", drafts, err)
	}
	if _, err = store.pool.Exec(ctx, `DELETE FROM sessions WHERE session_id = $1`, sessionID); err != nil {
		t.Fatal(err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM session_attachments WHERE attachment_id = $1`, attachmentID).Scan(&attachments); err != nil || attachments != 0 {
		t.Fatalf("attachments after %d %v", attachments, err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM draft_files WHERE draft_id = $1`, draftID).Scan(&drafts); err != nil || drafts != 0 {
		t.Fatalf("drafts after %d %v", drafts, err)
	}
}

func TestDelete_SessionRemovesMessages(t *testing.T) {
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
	userID := "user-del-" + suffix
	sessionID := "session-del-" + suffix
	token := "token-del-" + suffix
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM sessions WHERE session_id = $1`, sessionID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM auth_tokens WHERE user_id = $1`, userID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
	})
	if _, err = store.pool.Exec(ctx, `
		INSERT INTO users (user_id, login, password_hash, display_name, access_level)
		VALUES ($1, $2, 'x', 'Сессия', 1)`, userID, userID+"@corp.local"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.pool.Exec(ctx, `INSERT INTO auth_tokens (token, user_id) VALUES ($1, $2)`, token, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.pool.Exec(ctx, `INSERT INTO sessions (session_id, owner_user_id, title) VALUES ($1, $2, 'старый')`, sessionID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.pool.Exec(ctx, `INSERT INTO session_messages (session_id, position, body) VALUES ($1, 1, '{"role":"user","text":"вопрос"}')`, sessionID); err != nil {
		t.Fatal(err)
	}
	if err = store.Delete_Session(ctx, token, sessionID); err != nil {
		t.Fatal(err)
	}
	var messages int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM session_messages WHERE session_id = $1`, sessionID).Scan(&messages); err != nil || messages != 0 {
		t.Fatalf("messages %d %v", messages, err)
	}
	var sessions int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE session_id = $1`, sessionID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("sessions %d %v", sessions, err)
	}
}
