package source

import (
	"context"

	"docfinder/core/internal/texts"
)

const demoExternalID = "demo-instruction"

// Встроенный фрагмент: цикл синхронизации без сети и без папки
type DemoConnector struct{}

func (DemoConnector) Kind() string {
	return KindDemo
}

func (DemoConnector) Verify_Access(context.Context, ConnectorConfig) error {
	return nil
}

func (DemoConnector) List_Items(context.Context, string, ConnectorConfig) ([]RemoteItem, string, error) {
	return []RemoteItem{{
		ID:       demoExternalID,
		Title:    texts.SourceDemoTitle,
		Edition:  texts.EditionDefault,
		FileName: "demo.txt",
	}}, "", nil
}

func (DemoConnector) Fetch_Item(_ context.Context, item RemoteItem, _ ConnectorConfig) (FetchedFile, error) {
	body := []byte(texts.SourceDemoBody)
	return FetchedFile{
		ExternalID:  demoExternalID,
		Title:       texts.SourceDemoTitle,
		Edition:     texts.EditionDefault,
		FileName:    "demo.txt",
		ContentType: texts.PlainTextContentType,
		Body:        body,
		Checksum:    Checksum_Bytes(body),
	}, nil
}
