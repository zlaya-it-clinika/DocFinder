package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"docfinder/core/internal/texts"
)

func TestFolderConnectorReadsSupportedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("пункт 1.1"), 0o600); err != nil {
		t.Fatal(err)
	}
	connector := FolderConnector{}
	config := ConnectorConfig{Path: dir}
	if err := connector.Verify_Access(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	files, _, err := Pull_Documents(context.Background(), connector, "", config)
	if err != nil || len(files) != 1 || files[0].ExternalID != "note.txt" || string(files[0].Body) != "пункт 1.1" {
		t.Fatalf("files %+v err %v", files, err)
	}
	if Check_CanEnable(KindFolder, ConnectorConfig{}) == nil {
		t.Fatal("empty folder path enabled")
	}
}

func TestDemoConnectorIsStable(t *testing.T) {
	connector := DemoConnector{}
	files, _, err := Pull_Documents(context.Background(), connector, "", ConnectorConfig{})
	if err != nil || len(files) != 1 || files[0].Title != texts.SourceDemoTitle || files[0].Checksum == "" {
		t.Fatalf("demo %+v err %v", files, err)
	}
	again, _, err := Pull_Documents(context.Background(), connector, "", ConnectorConfig{})
	if err != nil || again[0].Checksum != files[0].Checksum {
		t.Fatalf("checksum changed %v %v", files[0].Checksum, again)
	}
}

func TestConsultantPlusLicensedHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret-key" {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/v1/status":
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(`{"ok":true}`))
		case request.URL.Path == "/v1/documents":
			_, _ = response.Write([]byte(`{"items":[{"id":"cp-1","title":"Приказ","edition":"2.0","fileName":"order.txt"}],"nextCursor":"c2"}`))
		case request.URL.Path == "/v1/documents/cp-1/file":
			response.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = response.Write([]byte("пункт 2.1"))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	connector := New_ConsultantPlus(server.Client())
	config := ConnectorConfig{BaseURL: server.URL, LicenseKey: "secret-key", ClientID: "app"}
	if err := connector.Verify_Access(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	files, cursor, err := Pull_Documents(context.Background(), connector, "", config)
	if err != nil || cursor != "c2" || len(files) != 1 || files[0].ExternalID != "cp-1" || string(files[0].Body) != "пункт 2.1" {
		t.Fatalf("pull %+v cursor %s err %v", files, cursor, err)
	}
	if err := connector.Verify_Access(context.Background(), ConnectorConfig{BaseURL: server.URL, LicenseKey: "wrong"}); !errors.Is(err, ErrLicenseDenied) {
		t.Fatalf("wrong key %v", err)
	}
	if err := Check_CanEnable(KindConsultantPlus, ConnectorConfig{}); err != ErrNotConfigured {
		t.Fatalf("empty consultant %v", err)
	}
}
