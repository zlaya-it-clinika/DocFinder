package livestore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/texts"
)

const counterAttachment = "attachment"

type SessionAttachment struct {
	AttachmentID string
	FileName     string
	Characters   int
}

type AttachmentFile struct {
	AttachmentID string
	FileName     string
}

type RevisionSource struct {
	AttachmentID string
	Title        string
	Text         string
	Files        []AttachmentFile
}

func (store *LiveStore) Create_SessionAttachment(ctx context.Context, token, sessionID, fileName, plainText string) (SessionAttachment, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return SessionAttachment{}, err
	}
	if !account.Has(texts.PermissionDraftsAssist) {
		return SessionAttachment{}, ErrAccessDenied
	}
	plainText = strings.TrimSpace(plainText)
	fileName = strings.TrimSpace(fileName)
	if plainText == "" || fileName == "" {
		return SessionAttachment{}, ErrInvalidBody
	}
	var ownerID string
	err = store.pool.QueryRow(ctx, `SELECT owner_user_id FROM sessions WHERE session_id = $1`, sessionID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionAttachment{}, ErrNotFound
	}
	if err != nil {
		return SessionAttachment{}, err
	}
	if ownerID != account.UserID {
		return SessionAttachment{}, ErrAccessDenied
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return SessionAttachment{}, err
	}
	defer tx.Rollback(ctx)
	number, err := next_IDTx(ctx, tx, counterAttachment)
	if err != nil {
		return SessionAttachment{}, err
	}
	attachmentID := "att-" + strconv.Itoa(number)
	characters := utf8.RuneCountInString(plainText)
	_, err = tx.Exec(ctx, `
		INSERT INTO session_attachments (attachment_id, session_id, owner_user_id, file_name, plain_text, characters)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		attachmentID, sessionID, account.UserID, fileName, plainText, characters)
	if err != nil {
		return SessionAttachment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SessionAttachment{}, err
	}
	return SessionAttachment{AttachmentID: attachmentID, FileName: fileName, Characters: characters}, nil
}

func (store *LiveStore) Find_RevisionSource(ctx context.Context, sessionID string, attachmentIDs []string) (RevisionSource, bool, error) {
	ids := clean_AttachmentIDs(attachmentIDs)
	if len(ids) == 0 {
		var currentID string
		err := store.pool.QueryRow(ctx, `SELECT COALESCE(draft_attachment_id, '') FROM sessions WHERE session_id = $1`, sessionID).Scan(&currentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return RevisionSource{}, false, ErrNotFound
		}
		if err != nil {
			return RevisionSource{}, false, err
		}
		if currentID == "" {
			return RevisionSource{}, false, nil
		}
		file, text, found, err := store.read_SessionAttachment(ctx, sessionID, currentID)
		if err != nil || !found {
			return RevisionSource{}, false, err
		}
		return RevisionSource{AttachmentID: file.AttachmentID, Title: file.FileName, Text: text, Files: []AttachmentFile{file}}, true, nil
	}
	parts := make([]string, 0, len(ids))
	files := make([]AttachmentFile, 0, len(ids))
	for _, attachmentID := range ids {
		file, text, found, err := store.read_SessionAttachment(ctx, sessionID, attachmentID)
		if err != nil {
			return RevisionSource{}, false, err
		}
		if !found {
			return RevisionSource{}, false, ErrInvalidBody
		}
		parts = append(parts, text)
		files = append(files, file)
	}
	return RevisionSource{
		AttachmentID: files[0].AttachmentID,
		Title:        files[0].FileName,
		Text:         strings.Join(parts, "\n\n"),
		Files:        files,
	}, true, nil
}

func (store *LiveStore) Store_RevisionText(ctx context.Context, sessionID, attachmentID, plainText string) error {
	tag, err := store.pool.Exec(ctx, `
		UPDATE session_attachments
		SET plain_text = $3, characters = $4
		WHERE attachment_id = $1 AND session_id = $2`,
		attachmentID, sessionID, plainText, utf8.RuneCountInString(plainText))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = store.pool.Exec(ctx, `UPDATE sessions SET draft_attachment_id = $2 WHERE session_id = $1`, sessionID, attachmentID)
	return err
}

func (store *LiveStore) read_SessionAttachment(ctx context.Context, sessionID, attachmentID string) (AttachmentFile, string, bool, error) {
	var file AttachmentFile
	var plainText string
	err := store.pool.QueryRow(ctx, `
		SELECT attachment_id, file_name, plain_text
		FROM session_attachments
		WHERE attachment_id = $1 AND session_id = $2`, attachmentID, sessionID).Scan(&file.AttachmentID, &file.FileName, &plainText)
	if errors.Is(err, pgx.ErrNoRows) {
		return AttachmentFile{}, "", false, nil
	}
	if err != nil {
		return AttachmentFile{}, "", false, err
	}
	return file, plainText, true, nil
}

func clean_AttachmentIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleaned = append(cleaned, id)
		}
	}
	return cleaned
}
