package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func TestStubSourcesCRUDAndDemoSync(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)

	if code := doJSON(t, handler, http.MethodGet, "/sources", employeeToken, nil).Code; code != http.StatusForbidden {
		t.Fatalf("employee list %d", code)
	}
	kinds := doJSON(t, handler, http.MethodGet, "/sources/kinds", adminToken, nil)
	if kinds.Code != http.StatusOK {
		t.Fatalf("kinds %d %s", kinds.Code, kinds.Body.String())
	}
	created := doJSON(t, handler, http.MethodPost, "/sources", adminToken, map[string]any{
		"kind": "demo", "title": "Демо", "intervalMinutes": 30,
	})
	if created.Code != http.StatusOK {
		t.Fatalf("create %d %s", created.Code, created.Body.String())
	}
	sourceID := decodeJSON[sourceResponse](t, created).SourceID
	if sourceID == "" || decodeJSON[sourceResponse](t, created).Enabled {
		t.Fatalf("created %+v", created.Body.String())
	}
	listed := doJSON(t, handler, http.MethodGet, "/sources", adminToken, nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list %d %s", listed.Code, listed.Body.String())
	}
	enabled := doJSON(t, handler, http.MethodPatch, "/sources/"+sourceID, adminToken, map[string]any{"enabled": true})
	if enabled.Code != http.StatusOK || !decodeJSON[sourceResponse](t, enabled).Enabled {
		t.Fatalf("enable %d %s", enabled.Code, enabled.Body.String())
	}
	if code := doJSON(t, handler, http.MethodPost, "/sources/"+sourceID+"/verify", adminToken, nil).Code; code != http.StatusOK {
		t.Fatalf("verify %d", code)
	}
	first := doJSON(t, handler, http.MethodPost, "/sources/"+sourceID+"/sync", adminToken, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("sync %d %s", first.Code, first.Body.String())
	}
	run := decodeJSON[sourceRunResponse](t, first)
	if run.Indexed != 1 {
		t.Fatalf("first sync %+v", run)
	}
	second := decodeJSON[sourceRunResponse](t, doJSON(t, handler, http.MethodPost, "/sources/"+sourceID+"/sync", adminToken, nil))
	if second.Skipped != 1 || second.Indexed != 0 {
		t.Fatalf("second sync %+v", second)
	}
	docs := doJSON(t, handler, http.MethodGet, "/documents", adminToken, nil)
	if docs.Code != http.StatusOK || !strings.Contains(docs.Body.String(), texts.SourceDemoTitle) {
		t.Fatalf("documents after sync %s", docs.Body.String())
	}
}

func TestStubFolderSourceAndConsultantEnableGuard(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rule.txt"), []byte("пункт 3.1 регистрация"), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := decodeJSON[sourceResponse](t, doJSON(t, handler, http.MethodPost, "/sources", adminToken, map[string]any{
		"kind": "folder", "title": "Папка", "config": map[string]string{"path": dir},
	}))
	if folder.Config.LicenseKeySet {
		t.Fatal("folder leaked license flag as set")
	}
	sync := decodeJSON[sourceRunResponse](t, doJSON(t, handler, http.MethodPost, "/sources/"+folder.SourceID+"/sync", adminToken, nil))
	if sync.Indexed != 1 {
		t.Fatalf("folder sync %+v", sync)
	}
	consultant := decodeJSON[sourceResponse](t, doJSON(t, handler, http.MethodPost, "/sources", adminToken, map[string]any{
		"kind": "consultantplus", "title": "СП",
	}))
	denied := doJSON(t, handler, http.MethodPatch, "/sources/"+consultant.SourceID, adminToken, map[string]any{"enabled": true})
	if denied.Code != http.StatusBadRequest {
		t.Fatalf("enable empty consultant %d %s", denied.Code, denied.Body.String())
	}
	unknown := doJSON(t, handler, http.MethodPost, "/sources", adminToken, map[string]any{"kind": "garant"})
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind %d", unknown.Code)
	}
}
