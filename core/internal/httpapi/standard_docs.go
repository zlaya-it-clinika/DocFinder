package httpapi

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"docfinder/core/internal/livestore"
	"docfinder/core/internal/source"
	"docfinder/core/internal/texts"
)

// Стандартный набор документов компании из STANDARD_DOCS_DIR: всегда в базе знаний с запуска.
// Пустой путь - набора нет. Каталог указан, но не читается - ошибка запуска, а не пустая база знаний
func sync_StandardDocuments(store *livestore.LiveStore, folder string) error {
	if folder == "" {
		return nil
	}
	listing, err := source.Read_Folder(folder)
	if err != nil {
		return errors.New(texts.LiveStandardDocsMissing + ": " + folder)
	}
	files := make([]livestore.StandardFile, 0, len(listing.Files))
	for _, file := range listing.Files {
		name := filepath.Base(file.Name)
		files = append(files, livestore.StandardFile{
			Title:       source.Title_FromFileName(name),
			FileName:    name,
			ContentType: contentType_ForFile(name),
			Body:        file.Body,
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return store.Sync_StandardDocuments(ctx, files)
}
