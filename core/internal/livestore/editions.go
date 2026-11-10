package livestore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Прежняя редакция документа в архиве. Текст архивной редакции в поиск не попадает
type ArchivedEdition struct {
	EditionID  string
	Edition    string
	FileName   string
	UploadedAt time.Time
	ArchivedAt time.Time
}

// Архив редакций виден тем же, кому виден сам документ. Новые архивные редакции идут первыми
func (store *LiveStore) List_DocumentEditions(ctx context.Context, token, documentID string) ([]ArchivedEdition, error) {
	if err := store.require_DocumentRead(ctx, token, documentID); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT edition_id, edition, file_name, uploaded_at, archived_at
		FROM document_editions WHERE document_id = $1
		ORDER BY archived_at DESC, edition_id DESC`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	editions := []ArchivedEdition{}
	for rows.Next() {
		var edition ArchivedEdition
		var editionID int64
		if err := rows.Scan(&editionID, &edition.Edition, &edition.FileName, &edition.UploadedAt, &edition.ArchivedAt); err != nil {
			return nil, err
		}
		edition.EditionID = strconv.FormatInt(editionID, 10)
		editions = append(editions, edition)
	}
	return editions, rows.Err()
}

func (store *LiveStore) Read_EditionFile(ctx context.Context, token, documentID, editionID string) (DocumentFile, error) {
	if err := store.require_DocumentRead(ctx, token, documentID); err != nil {
		return DocumentFile{}, err
	}
	number, err := strconv.ParseInt(editionID, 10, 64)
	if err != nil {
		return DocumentFile{}, ErrNotFound
	}
	var file DocumentFile
	err = store.pool.QueryRow(ctx, `
		SELECT file_name, content_type, body FROM document_editions
		WHERE document_id = $1 AND edition_id = $2`, documentID, number).Scan(&file.FileName, &file.ContentType, &file.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentFile{}, ErrNotFound
	}
	return file, err
}

func (store *LiveStore) require_DocumentRead(ctx context.Context, token, documentID string) error {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return err
	}
	var scope string
	var ownerID string
	var visibleToAll bool
	var documentRoleIDs []string
	err = store.pool.QueryRow(ctx, `
		SELECT scope, owner_id, visible_to_all,
			COALESCE((SELECT array_agg(role_id) FROM document_roles WHERE document_id = $1), '{}')
		FROM documents WHERE document_id = $1`, documentID).Scan(&scope, &ownerID, &visibleToAll, &documentRoleIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return document_ReadError(account, scope, ownerID, visibleToAll, documentRoleIDs)
}
