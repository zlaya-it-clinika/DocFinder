package source

import (
	"context"
	"path/filepath"
	"strings"

	"docfinder/core/internal/chunk"
)

// Каталог выгрузки: один уровень, как POST /documents/import
type FolderConnector struct{}

func (FolderConnector) Kind() string {
	return KindFolder
}

func (FolderConnector) Verify_Access(_ context.Context, config ConnectorConfig) error {
	_, err := Read_Folder(config.Path)
	return err
}

func (FolderConnector) List_Items(_ context.Context, _ string, config ConnectorConfig) ([]RemoteItem, string, error) {
	listing, err := Read_Folder(config.Path)
	if err != nil {
		return nil, "", err
	}
	items := make([]RemoteItem, 0, len(listing.Files))
	for _, file := range listing.Files {
		items = append(items, RemoteItem{
			ID:       file.Name,
			Title:    file.Name,
			FileName: file.Name,
		})
	}
	return items, "", nil
}

func (FolderConnector) Fetch_Item(_ context.Context, item RemoteItem, config ConnectorConfig) (FetchedFile, error) {
	listing, err := Read_Folder(config.Path)
	if err != nil {
		return FetchedFile{}, err
	}
	for _, file := range listing.Files {
		if file.Name != item.ID && file.Name != item.FileName {
			continue
		}
		name := file.Name
		return FetchedFile{
			ExternalID:  name,
			Title:       name,
			FileName:    name,
			ContentType: contentType_ForName(name),
			Body:        file.Body,
			Checksum:    Checksum_Bytes(file.Body),
		}, nil
	}
	return FetchedFile{}, ErrNotConfigured
}

func contentType_ForName(fileName string) string {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pdf":
		return "application/pdf"
	default:
		if chunk.Supports_DocumentText(fileName) {
			return "application/octet-stream"
		}
		return "application/octet-stream"
	}
}
