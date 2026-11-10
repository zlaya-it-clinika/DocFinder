package livestore

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/texts"
)

const counterDraft = "draft"

func (store *LiveStore) Save_DraftFile(ctx context.Context, ownerID, fileName string, body []byte) (string, error) {
	return store.insert_DraftFile(ctx, ownerID, "", fileName, body)
}

func (store *LiveStore) Save_SessionDraftFile(ctx context.Context, ownerID, sessionID, fileName string, body []byte) (string, error) {
	return store.insert_DraftFile(ctx, ownerID, sessionID, fileName, body)
}

// Пустой sessionID остается NULL: бланк фазы 9 не привязан к сессии.
func (store *LiveStore) insert_DraftFile(ctx context.Context, ownerID, sessionID, fileName string, body []byte) (string, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	number, err := next_IDTx(ctx, tx, counterDraft)
	if err != nil {
		return "", err
	}
	draftID := "draft-" + strconv.Itoa(number)
	_, err = tx.Exec(ctx, `
		INSERT INTO draft_files (draft_id, owner_user_id, file_name, content_type, body, session_id)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
		draftID, ownerID, fileName, texts.DocxContentType, body, sessionID)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return draftID, nil
}

func (store *LiveStore) Read_DraftFile(ctx context.Context, userID, draftID string) (DocumentFile, error) {
	var ownerID string
	var file DocumentFile
	err := store.pool.QueryRow(ctx, `
		SELECT owner_user_id, file_name, content_type, body
		FROM draft_files WHERE draft_id = $1`, draftID).Scan(&ownerID, &file.FileName, &file.ContentType, &file.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentFile{}, ErrNotFound
	}
	if err != nil {
		return DocumentFile{}, err
	}
	if ownerID != userID {
		return DocumentFile{}, ErrAccessDenied
	}
	return file, nil
}
