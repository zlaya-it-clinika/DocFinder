package livestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/texts"
)

// Файл стандартного набора документов компании (source/docs)
type StandardFile struct {
	Title       string
	FileName    string
	ContentType string
	Body        []byte
}

// Стандартный набор при каждом запуске ядра: недостающий документ загружается, новая версия файла в наборе
// заменяет документ (прежняя редакция уходит в архив), неизменный файл не трогается.
// Документы общие, видны всем, процесс «Документооборот». Фрагменты сохраняются без вектора,
// векторы досчитывает Start_PendingReindex, поэтому запуск не ждет Ollama
func (store *LiveStore) Sync_StandardDocuments(ctx context.Context, files []StandardFile) error {
	for _, file := range files {
		sum := sha256.Sum256(file.Body)
		hash := hex.EncodeToString(sum[:])
		var documentID string
		var savedHash string
		var body []byte
		err := store.pool.QueryRow(ctx, `
			SELECT document_id, standard_hash, body FROM documents
			WHERE title = $1 AND scope = 'global'`, file.Title).Scan(&documentID, &savedHash, &body)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		case savedHash == hash || (savedHash == "" && bytes.Equal(body, file.Body)):
			// Файл набора не менялся. Документ, загруженный раньше вручную тем же файлом, просто входит в набор.
			// Редакция, которую администратор загрузил поверх, тоже остается: хеш набора тот же
			if _, err := store.pool.Exec(ctx, `UPDATE documents SET standard = true, standard_hash = $2 WHERE document_id = $1`, documentID, hash); err != nil {
				return err
			}
			continue
		}
		result, err := store.index_Document(ctx, UploadInput{
			FileName:     file.FileName,
			ContentType:  file.ContentType,
			Body:         file.Body,
			Title:        file.Title,
			Edition:      texts.EditionDefault,
			AccessLevel:  texts.SeedDocumentAccessOpen,
			Scope:        texts.ScopeGlobal,
			ProcessID:    texts.ProcessDocuments,
			VisibleToAll: true,
			RoleIDs:      []string{},
			Standard:     true,
			StandardHash: hash,
		}, false)
		if errors.Is(err, ErrUnsupportedFile) {
			log.Printf("standard document skipped, unsupported file: %s", file.FileName)
			continue
		}
		if err != nil {
			return err
		}
		log.Printf("standard document loaded: %s (%s)", file.Title, result.DocumentID)
	}
	return nil
}
