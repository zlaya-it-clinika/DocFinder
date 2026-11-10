package httpapi

import (
	"net/http"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

// Начало настоящего PNG: по нему ядро определяет тип картинки
const testPNG = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89"

func TestOrganizationOfficialsAndPermissions(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)

	empty := decodeJSON[organizationResponse](t, doJSON(t, handler, http.MethodGet, "/organization", employeeToken, nil))
	if len(empty.Officials) != 0 || empty.CanManage || len(empty.Images) != 2 || empty.Images[imageKindEmblem] != nil || empty.Images[imageKindStamp] != nil {
		t.Fatalf("empty organization %+v", empty)
	}
	if unauthorized := doJSON(t, handler, http.MethodGet, "/organization", "", nil); unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("no token %d", unauthorized.Code)
	}
	chiefBody := map[string]any{"position": "Генеральный директор", "fullName": "Петров А. Б.", "chief": true, "validFrom": "2026-11-01"}
	if denied := doJSON(t, handler, http.MethodPost, "/organization/officials", employeeToken, chiefBody); denied.Code != http.StatusForbidden {
		t.Fatalf("employee create %d", denied.Code)
	}

	first := decodeJSON[officialResponse](t, doJSON(t, handler, http.MethodPost, "/organization/officials", adminToken, chiefBody))
	if first.OfficialID == "" || !first.Chief || first.ValidFrom != "2026-11-01" || first.FullName != "Петров А. Б." {
		t.Fatalf("first chief %+v", first)
	}
	deputy := decodeJSON[officialResponse](t, doJSON(t, handler, http.MethodPost, "/organization/officials", adminToken, map[string]any{
		"position": "Директор Департамента ДОУ", "fullName": "Иванов И. И.", "department": "Департамент ДОУ",
	}))
	// Новый генеральный директор снимает отметку с прежнего
	second := decodeJSON[officialResponse](t, doJSON(t, handler, http.MethodPost, "/organization/officials", adminToken, map[string]any{
		"position": "Генеральный директор", "fullName": "Сидорова В. Г.", "chief": true,
	}))
	listed := decodeJSON[organizationResponse](t, doJSON(t, handler, http.MethodGet, "/organization", adminToken, nil))
	if !listed.CanManage || len(listed.Officials) != 3 || listed.Officials[0].OfficialID != second.OfficialID || !listed.Officials[0].Chief {
		t.Fatalf("listed %+v", listed)
	}
	for _, official := range listed.Officials[1:] {
		if official.Chief {
			t.Fatalf("two chiefs %+v", listed.Officials)
		}
	}

	updated := decodeJSON[officialResponse](t, doJSON(t, handler, http.MethodPatch, "/organization/officials/"+deputy.OfficialID, adminToken, map[string]any{
		"position": "Директор Департамента ДОУ", "fullName": "Иванов Иван Иванович", "department": "Департамент ДОУ", "validFrom": "2026-12-01",
	}))
	if updated.FullName != "Иванов Иван Иванович" || updated.ValidFrom != "2026-12-01" || updated.Chief {
		t.Fatalf("updated %+v", updated)
	}
	for _, bad := range []map[string]any{
		{"position": "", "fullName": "Без должности"},
		{"position": "Должность", "fullName": " "},
		{"position": "Должность", "fullName": "ФИО", "validFrom": "01.11.2026"},
	} {
		if response := doJSON(t, handler, http.MethodPost, "/organization/officials", adminToken, bad); response.Code != http.StatusBadRequest {
			t.Fatalf("bad body %+v status %d", bad, response.Code)
		}
	}
	if missing := doJSON(t, handler, http.MethodPatch, "/organization/officials/official-404", adminToken, chiefBody); missing.Code != http.StatusNotFound {
		t.Fatalf("missing update %d", missing.Code)
	}
	if deleted := doJSON(t, handler, http.MethodDelete, "/organization/officials/"+first.OfficialID, adminToken, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete %d", deleted.Code)
	}
	if again := doJSON(t, handler, http.MethodDelete, "/organization/officials/"+first.OfficialID, adminToken, nil); again.Code != http.StatusNotFound {
		t.Fatalf("delete again %d", again.Code)
	}
}

func TestOrganizationImages(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)

	if denied := uploadMultipart(t, handler, "/organization/images/stamp", employeeToken, "stamp.png", testPNG, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("employee upload %d", denied.Code)
	}
	if text := uploadMultipart(t, handler, "/organization/images/stamp", adminToken, "stamp.png", "не картинка", nil); text.Code != http.StatusBadRequest || decodeJSON[errorBody](t, text).Error != texts.ErrorImageInvalid {
		t.Fatalf("text as image %d %s", text.Code, text.Body.String())
	}
	if unknown := uploadMultipart(t, handler, "/organization/images/logo", adminToken, "logo.png", testPNG, nil); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown kind %d", unknown.Code)
	}

	saved := decodeJSON[organizationImageResponse](t, uploadMultipart(t, handler, "/organization/images/stamp", adminToken, "Штамп.png", testPNG, nil))
	if saved.Kind != imageKindStamp || saved.ContentType != "image/png" || saved.Size != len(testPNG) || saved.FileName != "Штамп.png" || saved.Link != "/organization/images/stamp" {
		t.Fatalf("saved %+v", saved)
	}
	listed := decodeJSON[organizationResponse](t, doJSON(t, handler, http.MethodGet, "/organization", employeeToken, nil))
	if listed.Images[imageKindStamp] == nil || listed.Images[imageKindEmblem] != nil {
		t.Fatalf("images %+v", listed.Images)
	}

	// Картинку видит любой сотрудник: она нужна бланкам
	image := doRaw(t, handler, http.MethodGet, "/organization/images/stamp", employeeToken, nil, "")
	if image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/png" || image.Body.String() != testPNG {
		t.Fatalf("image %d %q", image.Code, image.Header().Get("Content-Type"))
	}
	if absent := doRaw(t, handler, http.MethodGet, "/organization/images/emblem", employeeToken, nil, ""); absent.Code != http.StatusNotFound {
		t.Fatalf("absent emblem %d", absent.Code)
	}
	if denied := doJSON(t, handler, http.MethodDelete, "/organization/images/stamp", employeeToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("employee delete %d", denied.Code)
	}
	if deleted := doJSON(t, handler, http.MethodDelete, "/organization/images/stamp", adminToken, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete %d", deleted.Code)
	}
	if gone := doRaw(t, handler, http.MethodGet, "/organization/images/stamp", employeeToken, nil, ""); gone.Code != http.StatusNotFound {
		t.Fatalf("deleted image %d", gone.Code)
	}
}
