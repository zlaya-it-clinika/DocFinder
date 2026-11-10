package livestore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/texts"
)

type AuditLine struct {
	EntryID     string
	UserID      string
	DisplayName string
	Question    string
	CreatedAt   time.Time
	Documents   []byte
}

func (store *LiveStore) Create_Session(ctx context.Context, token string) (string, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return "", err
	}
	number, err := store.next_ID(ctx, counterSession)
	if err != nil {
		return "", err
	}
	sessionID := "session-" + strconv.Itoa(number)
	now := time.Now().UTC()
	_, err = store.pool.Exec(ctx, `
		INSERT INTO sessions (session_id, owner_user_id, template_step, title, created_at, updated_at)
		VALUES ($1, $2, 0, '', $3, $3)`, sessionID, account.UserID, now)
	return sessionID, err
}

type SessionLine struct {
	SessionID string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (store *LiveStore) List_Sessions(ctx context.Context, token string) ([]SessionLine, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT session_id, title, created_at, updated_at
		FROM sessions
		WHERE owner_user_id = $1
		ORDER BY updated_at DESC, session_id DESC`, account.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []SessionLine{}
	for rows.Next() {
		var line SessionLine
		if err := rows.Scan(&line.SessionID, &line.Title, &line.CreatedAt, &line.UpdatedAt); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

func (store *LiveStore) List_SessionMessages(ctx context.Context, token, sessionID string) ([][]byte, error) {
	if _, _, _, err := store.Load_SessionForMessage(ctx, token, sessionID); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT body FROM session_messages WHERE session_id = $1 ORDER BY position`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := [][]byte{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		messages = append(messages, body)
	}
	return messages, rows.Err()
}

func (store *LiveStore) Load_SessionForMessage(ctx context.Context, token, sessionID string) (Account, int, []byte, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return Account{}, 0, nil, err
	}
	var ownerID string
	var templateStep int
	var templateState []byte
	err = store.pool.QueryRow(ctx, `
		SELECT owner_user_id, template_step, template_state FROM sessions WHERE session_id = $1`, sessionID).Scan(&ownerID, &templateStep, &templateState)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, 0, nil, ErrNotFound
	}
	if err != nil {
		return Account{}, 0, nil, err
	}
	if ownerID != account.UserID {
		return Account{}, 0, nil, ErrAccessDenied
	}
	return account, templateStep, templateState, nil
}

func (store *LiveStore) Rename_Session(ctx context.Context, token, sessionID, title string) (SessionLine, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return SessionLine{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return SessionLine{}, ErrInvalidBody
	}
	var line SessionLine
	err = store.pool.QueryRow(ctx, `
		UPDATE sessions SET title = $3, updated_at = now()
		WHERE session_id = $1 AND owner_user_id = $2
		RETURNING session_id, title, created_at, updated_at`, sessionID, account.UserID, title).Scan(&line.SessionID, &line.Title, &line.CreatedAt, &line.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if accessError := store.session_Access(ctx, sessionID, account.UserID); accessError != nil {
			return SessionLine{}, accessError
		}
		return SessionLine{}, ErrNotFound
	}
	if err != nil {
		return SessionLine{}, err
	}
	return line, nil
}

func (store *LiveStore) Delete_Session(ctx context.Context, token, sessionID string) error {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return err
	}
	if err = store.session_Access(ctx, sessionID, account.UserID); err != nil {
		return err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Сообщения не каскадом: иначе удаление сессии упрется во внешний ключ
	if _, err = tx.Exec(ctx, `DELETE FROM session_messages WHERE session_id = $1`, sessionID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM sessions WHERE session_id = $1 AND owner_user_id = $2`, sessionID, account.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (store *LiveStore) session_Access(ctx context.Context, sessionID, userID string) error {
	var ownerID string
	err := store.pool.QueryRow(ctx, `SELECT owner_user_id FROM sessions WHERE session_id = $1`, sessionID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ownerID != userID {
		return ErrAccessDenied
	}
	return nil
}

func (store *LiveStore) Save_SessionExchange(ctx context.Context, sessionID string, templateStep int, templateState, userBody, assistantBody, auditDocuments []byte, userID, displayName, question, title string) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	if len(templateState) == 0 {
		templateState = []byte("{}")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE sessions
		SET template_step = $2, template_state = $3::jsonb, updated_at = $4, title = CASE WHEN title = '' THEN $5 ELSE title END
		WHERE session_id = $1`, sessionID, templateStep, string(templateState), now, title); err != nil {
		return err
	}
	var position int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) FROM session_messages WHERE session_id = $1`, sessionID).Scan(&position); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO session_messages (session_id, position, body) VALUES ($1, $2, $3::jsonb)`, sessionID, position+1, string(userBody)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO session_messages (session_id, position, body) VALUES ($1, $2, $3::jsonb)`, sessionID, position+2, string(assistantBody)); err != nil {
		return err
	}
	number, err := next_IDTx(ctx, tx, counterAudit)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_entries (entry_id, user_id, display_name, question, created_at, documents)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		"audit-"+strconv.Itoa(number), userID, displayName, question, now, string(auditDocuments))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *LiveStore) List_Audit(ctx context.Context, token string) ([]AuditLine, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionAuditView); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT entry_id, user_id, display_name, question, created_at, documents
		FROM audit_entries
		ORDER BY created_at ASC, entry_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []AuditLine{}
	for rows.Next() {
		var entry AuditLine
		if err := rows.Scan(&entry.EntryID, &entry.UserID, &entry.DisplayName, &entry.Question, &entry.CreatedAt, &entry.Documents); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// Режим чата сессии: read или plan (texts.ChatMode...)
func (store *LiveStore) Read_SessionMode(ctx context.Context, sessionID string) (string, error) {
	var mode string
	err := store.pool.QueryRow(ctx, `SELECT chat_mode FROM sessions WHERE session_id = $1`, sessionID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return mode, err
}

func (store *LiveStore) Save_SessionMode(ctx context.Context, sessionID, mode string) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET chat_mode = $2 WHERE session_id = $1`, sessionID, mode)
	return err
}
