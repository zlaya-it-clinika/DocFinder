package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"docfinder/core/internal/chunk"
	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func TestStubDialogDocumentsRolesAndAudit(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)

	me := doJSON(t, handler, http.MethodGet, "/me", employeeToken, nil)
	if me.Code != http.StatusOK {
		t.Fatalf("me status %d", me.Code)
	}
	profile := decodeJSON[profileResponse](t, me)
	if profile.UserID != texts.SeedEmployeeID || profile.AccessLevel != texts.SeedEmployeeAccessLevel || profile.DisplayName != texts.SeedEmployeeDisplayName || profile.Role != texts.RoleUser {
		t.Fatalf("profile %+v", profile)
	}

	sessionID := createSession(t, handler, employeeToken)
	answer := postMessage(t, handler, employeeToken, sessionID, "Как зарегистрировать входящий документ?")
	if answer.Kind != texts.KindFallback || answer.Text != texts.FallbackText || answer.RelevancePercent != 0 || len(answer.Citations) != 0 {
		t.Fatalf("answer %+v", answer)
	}
	if answer.Contacts == nil || len(*answer.Contacts) != 0 {
		t.Fatalf("contacts %+v", answer.Contacts)
	}
	if answer.Role != "" {
		t.Fatalf("post message should omit role, got %s", answer.Role)
	}
	listed := doJSON(t, handler, http.MethodGet, "/sessions", employeeToken, nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("sessions %d %s", listed.Code, listed.Body.String())
	}
	var sessionList struct {
		Sessions []sessionLine `json:"sessions"`
	}
	decodeInto(t, listed, &sessionList)
	if len(sessionList.Sessions) != 1 || sessionList.Sessions[0].SessionID != sessionID || sessionList.Sessions[0].Title == "" {
		t.Fatalf("session list %+v", sessionList.Sessions)
	}

	clarify := postMessage(t, handler, employeeToken, sessionID, "Уточни тип документа")
	if clarify.Kind != texts.KindClarify || len(clarify.Citations) != 0 || clarify.RelevancePercent != 0 || clarify.Contacts != nil {
		t.Fatalf("clarify %+v", clarify)
	}
	fallback := postMessage(t, handler, employeeToken, sessionID, "Этого не найдено")
	if fallback.Kind != texts.KindFallback || len(fallback.Citations) != 0 || fallback.Contacts == nil || len(*fallback.Contacts) != 0 {
		t.Fatalf("fallback %+v", fallback)
	}
	denied := postMessage(t, handler, employeeToken, sessionID, "Нет доступа к регламенту")
	if denied.Kind != texts.KindAccessDenied || denied.Contacts != nil {
		t.Fatalf("denied %+v", denied)
	}

	templateSession := createSession(t, handler, employeeToken)
	// Служебной записки нет среди бланков модулей: в режиме «План» ее готовит макет, отмена возвращает к обычному чату
	noteLayout := postModeMessage(t, handler, employeeToken, templateSession, "Нужен шаблон служебной записки", texts.ChatModePlan)
	if noteLayout.Kind != texts.KindTemplateField || noteLayout.Template == nil || noteLayout.Template.ID != "service-note" || len(noteLayout.Citations) != 0 {
		t.Fatalf("note layout %+v", noteLayout)
	}
	if cancelled := postMessage(t, handler, employeeToken, templateSession, "отмена"); cancelled.Text != texts.LayoutCancelledText {
		t.Fatalf("layout cancel %+v", cancelled)
	}
	field := postMessage(t, handler, employeeToken, templateSession, "Нужен модуль распоряжения о создании рабочей группы")
	if field.Kind != texts.KindTemplateField || field.Contacts != nil || field.Template == nil || field.Template.Field != "1" || !strings.Contains(field.Template.Title, "рабочей группы") || field.Template.FileLink != "/blanks/order-work-group/file" {
		t.Fatalf("template field %+v", field)
	}
	blankFile := doRaw(t, handler, http.MethodGet, field.Template.FileLink, employeeToken, nil, "")
	if blankFile.Code != http.StatusOK || !strings.HasPrefix(blankFile.Body.String(), "PK") {
		t.Fatalf("blank file %d", blankFile.Code)
	}
	missingBlank := doRaw(t, handler, http.MethodGet, "/blanks/missing/file", employeeToken, nil, "")
	if missingBlank.Code != http.StatusNotFound {
		t.Fatalf("missing blank %d", missingBlank.Code)
	}
	next := postMessage(t, handler, employeeToken, templateSession, "не найдено")
	if next.Kind != texts.KindTemplateField || next.Template == nil || next.Template.Field == "1" {
		t.Fatalf("next field %+v", next)
	}
	reply := postMessage(t, handler, employeeToken, templateSession, "проверка архива")
	for guard := 0; reply.Kind == texts.KindTemplateField && guard < 20; guard++ {
		reply = postMessage(t, handler, employeeToken, templateSession, "значение")
	}
	if reply.Kind != texts.KindTemplateReady || reply.Template == nil || reply.RelevancePercent != 0 || len(reply.Citations) != 0 || reply.Template.FileLink != "/blanks/order-work-group/file" || reply.Template.DraftLink == "" {
		t.Fatalf("template ready %+v", reply)
	}
	if !strings.Contains(reply.Template.DraftText, "не найдено") || !strings.Contains(reply.Template.DraftText, "проверка архива") || strings.Contains(reply.Template.DraftText, "[1]") {
		t.Fatalf("draft %s", reply.Template.DraftText)
	}
	draftFile := doRaw(t, handler, http.MethodGet, reply.Template.DraftLink, employeeToken, nil, "")
	if draftFile.Code != http.StatusOK || !strings.HasPrefix(draftFile.Body.String(), "PK") {
		t.Fatalf("draft file %d", draftFile.Code)
	}
	draftPlain, draftErr := chunk.Extract_DocumentText("draft.docx", draftFile.Body.Bytes())
	if draftErr != nil || !strings.Contains(draftPlain, "не найдено") || strings.Contains(strings.ToLower(draftPlain), "примечани") {
		t.Fatalf("draft docx %v %s", draftErr, draftPlain)
	}
	foreignDraft := doRaw(t, handler, http.MethodGet, reply.Template.DraftLink, adminToken, nil, "")
	if foreignDraft.Code != http.StatusForbidden {
		t.Fatalf("foreign draft %d", foreignDraft.Code)
	}

	historyRec := doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, employeeToken, nil)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status %d", historyRec.Code)
	}
	var history struct {
		SessionID string `json:"sessionId"`
		Messages  []struct {
			Role string `json:"role"`
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"messages"`
	}
	decodeInto(t, historyRec, &history)
	if history.SessionID != sessionID || len(history.Messages) != 8 || history.Messages[0].Role != "user" || history.Messages[1].Role != "assistant" {
		t.Fatalf("history %+v", history)
	}

	adminHistory := doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, adminToken, nil)
	if adminHistory.Code != http.StatusForbidden {
		t.Fatalf("foreign session %d", adminHistory.Code)
	}
	missing := doJSON(t, handler, http.MethodGet, "/sessions/session-missing", employeeToken, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing session %d", missing.Code)
	}

	employeeDocs := listDocuments(t, handler, employeeToken)
	if len(employeeDocs) != 1 || employeeDocs[0].DocumentID != texts.SeedInstructionID {
		t.Fatalf("employee docs %+v", employeeDocs)
	}
	adminDocs := listDocuments(t, handler, adminToken)
	if len(adminDocs) != 2 {
		t.Fatalf("admin docs %+v", adminDocs)
	}

	openFile := doRaw(t, handler, http.MethodGet, "/documents/"+texts.SeedInstructionID+"/file", employeeToken, nil, "")
	if openFile.Code != http.StatusOK || openFile.Body.String() != texts.SeedInstructionBody {
		t.Fatalf("open file %d %s", openFile.Code, openFile.Body.String())
	}
	closedFile := doRaw(t, handler, http.MethodGet, "/documents/"+texts.SeedRestrictedID+"/file", employeeToken, nil, "")
	if closedFile.Code != http.StatusForbidden {
		t.Fatalf("closed file %d", closedFile.Code)
	}
	adminFile := doRaw(t, handler, http.MethodGet, "/documents/"+texts.SeedRestrictedID+"/file", adminToken, nil, "")
	if adminFile.Code != http.StatusOK || adminFile.Body.String() != texts.SeedRestrictedBody {
		t.Fatalf("admin file %d", adminFile.Code)
	}

	upload := uploadDocument(t, handler, employeeToken, "note.txt", "hello", "Новый регламент", "1.0", "1")
	if upload.Status != texts.StatusAccepted || upload.DocumentID == "" {
		t.Fatalf("upload %+v", upload)
	}
	if !hasDocument(listDocuments(t, handler, employeeToken), upload.DocumentID) {
		t.Fatal("uploaded document is missing from the list")
	}
	replaced := uploadDocument(t, handler, adminToken, "instruction-2.txt", "edition two", texts.SeedInstructionTitle, "2.0", "1")
	if replaced.DocumentID != texts.SeedInstructionID || replaced.Edition != "2.0" {
		t.Fatalf("replace %+v", replaced)
	}
	replacedFile := doRaw(t, handler, http.MethodGet, "/documents/"+texts.SeedInstructionID+"/file", employeeToken, nil, "")
	if replacedFile.Body.String() != "edition two" {
		t.Fatalf("replaced body %s", replacedFile.Body.String())
	}

	hidden := uploadDocument(t, handler, adminToken, "secret.txt", "secret", "Секрет", "1.0", "2")
	if hasDocument(listDocuments(t, handler, employeeToken), hidden.DocumentID) {
		t.Fatal("employee sees a closed upload")
	}
	if !hasDocument(listDocuments(t, handler, adminToken), hidden.DocumentID) {
		t.Fatal("admin does not see a closed upload")
	}

	employeeAudit := doJSON(t, handler, http.MethodGet, "/audit", employeeToken, nil)
	if employeeAudit.Code != http.StatusForbidden {
		t.Fatalf("employee audit %d", employeeAudit.Code)
	}
	auditRec := doJSON(t, handler, http.MethodGet, "/audit", adminToken, nil)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("audit status %d", auditRec.Code)
	}
	var auditBody struct {
		Entries []auditEntry `json:"entries"`
	}
	decodeInto(t, auditRec, &auditBody)
	if len(auditBody.Entries) == 0 || auditBody.Entries[0].Question != "Как зарегистрировать входящий документ?" {
		t.Fatalf("audit %+v", auditBody.Entries)
	}
	if len(auditBody.Entries[0].Documents) != 0 {
		t.Fatalf("audit documents %+v", auditBody.Entries[0].Documents)
	}

	patched := doJSON(t, handler, http.MethodPatch, "/users/"+texts.SeedEmployeeID, adminToken, map[string]any{
		"displayName": "Сотрудник архива",
		"accessLevel": 2,
	})
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status %d body %s", patched.Code, patched.Body.String())
	}
	var patchedUser accountResponse
	decodeInto(t, patched, &patchedUser)
	if patchedUser.DisplayName != "Сотрудник архива" || patchedUser.AccessLevel != 2 || patchedUser.Role != texts.RoleAdmin {
		t.Fatalf("patched %+v", patchedUser)
	}
	if !hasDocument(listDocuments(t, handler, employeeToken), texts.SeedRestrictedID) {
		t.Fatal("raised access level still hides the closed document")
	}

	created := doJSON(t, handler, http.MethodPost, "/users", adminToken, map[string]any{
		"login":       "clerk@corp.local",
		"password":    "clerk",
		"displayName": "Делопроизводитель",
		"accessLevel": 1,
	})
	if created.Code != http.StatusOK {
		t.Fatalf("create user %d %s", created.Code, created.Body.String())
	}
	duplicate := doJSON(t, handler, http.MethodPost, "/users", adminToken, map[string]any{
		"login":       "clerk@corp.local",
		"password":    "other",
		"displayName": "Другой",
	})
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate login %d", duplicate.Code)
	}
	clerkToken := login(t, handler, "clerk@corp.local", "clerk")
	if clerkToken == "" {
		t.Fatal("created user cannot login")
	}
	employeeUsers := doJSON(t, handler, http.MethodGet, "/users", clerkToken, nil)
	if employeeUsers.Code != http.StatusForbidden {
		t.Fatalf("clerk users %d", employeeUsers.Code)
	}

	draft := uploadDraft(t, handler, employeeToken)
	if draft.Code != http.StatusOK || !strings.Contains(draft.Body.String(), texts.DraftRecommendation) || !strings.Contains(draft.Body.String(), "/documents/"+texts.SeedInstructionID+"/file#p") {
		t.Fatalf("draft %d %s", draft.Code, draft.Body.String())
	}
	completeDraft := uploadMultipart(t, handler, "/drafts/check", employeeToken, "ready.txt", "Приложение № 1. Подпись Иванова.", nil)
	if completeDraft.Code != http.StatusOK {
		t.Fatalf("complete draft %d %s", completeDraft.Code, completeDraft.Body.String())
	}
	var completeBody struct {
		Recommendations []draftRecommendation `json:"recommendations"`
	}
	decodeInto(t, completeDraft, &completeBody)
	if len(completeBody.Recommendations) != 0 {
		t.Fatalf("complete recommendations %+v", completeBody.Recommendations)
	}
	rejectedDraft := uploadMultipart(t, handler, "/drafts/check", employeeToken, "sheet.xlsx", "нет", nil)
	if rejectedDraft.Code != http.StatusBadRequest {
		t.Fatalf("unsupported draft %d", rejectedDraft.Code)
	}

	badPassword := doJSON(t, handler, http.MethodPost, "/auth/login", "", map[string]string{
		"login":    texts.SeedEmployeeLogin,
		"password": "wrong",
	})
	if badPassword.Code != http.StatusUnauthorized {
		t.Fatalf("bad password %d", badPassword.Code)
	}
	if employeeToken != "" && !strings.Contains(badPassword.Header().Get("Access-Control-Allow-Origin"), "*") {
		t.Fatal("missing CORS header")
	}
}

func TestLogoutRevokesToken(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	token := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout %d %s", response.Code, response.Body.String())
	}
	me := doJSON(t, handler, http.MethodGet, "/me", token, nil)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("token still works %d", me.Code)
	}
}

func TestLiveModeRequiresDatabase(t *testing.T) {
	_, err := NewHandler(config.Config{Mode: config.ModeLive})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("live startup error %v", err)
	}
}

func TestDebugLogSkipsSecrets(t *testing.T) {
	var logs bytes.Buffer
	original := logWriter()
	setLogWriter(&logs)
	defer setLogWriter(original)

	handler := must_Handler(t, config.Config{Mode: config.ModeStub, Debug: true})
	response := doJSON(t, handler, http.MethodPost, "/auth/login", "", map[string]string{
		"login":    texts.SeedEmployeeLogin,
		"password": texts.SeedEmployeePassword,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("login %d", response.Code)
	}
	line := logs.String()
	if strings.Contains(line, texts.SeedEmployeePassword) || strings.Contains(line, "Bearer") {
		t.Fatalf("debug log leaked secrets: %s", line)
	}
	if !strings.Contains(line, "POST /auth/login 200") {
		t.Fatalf("debug log %s", line)
	}
}

func TestDocumentScopeListUploadAndDelete(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)

	employeeList := listDocumentPage(t, handler, employeeToken, "")
	if employeeList.CanUploadGlobal || !employeeList.CanUploadPersonal || len(employeeList.Documents) != 1 {
		t.Fatalf("employee list %+v", employeeList)
	}
	if employeeList.Documents[0].Scope != texts.ScopeGlobal || employeeList.Documents[0].CanDelete || employeeList.Documents[0].OwnerID != "" {
		t.Fatalf("seed line %+v", employeeList.Documents[0])
	}
	adminList := listDocumentPage(t, handler, adminToken, "")
	if !adminList.CanUploadGlobal || !adminList.CanUploadPersonal || len(adminList.Documents) != 2 {
		t.Fatalf("admin list %+v", adminList)
	}
	// Инструкция заглушки - стандартный набор: администратор ее не удаляет
	for _, document := range adminList.Documents {
		if document.CanDelete == document.Standard || document.Scope != texts.ScopeGlobal || document.Standard != (document.DocumentID == texts.SeedInstructionID) {
			t.Fatalf("admin seed %+v", document)
		}
	}

	globalDenied := uploadMultipart(t, handler, "/documents", employeeToken, "global.txt", "text", map[string]string{
		"title": "Общий от сотрудника",
		"scope": texts.ScopeGlobal,
	})
	if globalDenied.Code != http.StatusForbidden {
		t.Fatalf("employee global upload %d %s", globalDenied.Code, globalDenied.Body.String())
	}

	personal := uploadMultipart(t, handler, "/documents", employeeToken, "mine.txt", "мой текст", map[string]string{
		"title":       texts.SeedInstructionTitle,
		"edition":     "9.0",
		"scope":       texts.ScopePersonal,
		"accessLevel": "2",
	})
	if personal.Code != http.StatusOK {
		t.Fatalf("personal upload %d %s", personal.Code, personal.Body.String())
	}
	uploaded := decodeJSON[uploadResponse](t, personal)
	if uploaded.DocumentID == texts.SeedInstructionID || uploaded.Scope != texts.ScopePersonal {
		t.Fatalf("personal result %+v", uploaded)
	}
	ownPage := listDocumentPage(t, handler, employeeToken, "personal")
	if len(ownPage.Documents) != 1 || ownPage.Documents[0].DocumentID != uploaded.DocumentID || ownPage.Documents[0].AccessLevel != texts.SeedEmployeeAccessLevel {
		t.Fatalf("own page %+v", ownPage.Documents)
	}
	globalPage := listDocumentPage(t, handler, employeeToken, "global")
	if hasDocument(globalPage.Documents, uploaded.DocumentID) || !hasDocument(globalPage.Documents, texts.SeedInstructionID) {
		t.Fatalf("global page %+v", globalPage.Documents)
	}
	if ownPage.Documents[0].OwnerID != texts.SeedEmployeeID || ownPage.Documents[0].OwnerName != texts.SeedEmployeeDisplayName || !ownPage.Documents[0].CanDelete || ownPage.Documents[0].UploadedAt == "" {
		t.Fatalf("own line %+v", ownPage.Documents[0])
	}
	if hasDocument(listDocumentPage(t, handler, adminToken, "").Documents, uploaded.DocumentID) {
		t.Fatal("admin default list includes a foreign personal document")
	}
	allDenied := doJSON(t, handler, http.MethodGet, "/documents?scope=all", employeeToken, nil)
	if allDenied.Code != http.StatusForbidden {
		t.Fatalf("employee scope all %d", allDenied.Code)
	}
	allPage := listDocumentPage(t, handler, adminToken, "all")
	if !hasDocument(allPage.Documents, uploaded.DocumentID) || !hasDocument(allPage.Documents, texts.SeedRestrictedID) {
		t.Fatalf("admin all %+v", allPage.Documents)
	}

	foreignFile := doRaw(t, handler, http.MethodGet, "/documents/"+uploaded.DocumentID+"/file", adminToken, nil, "")
	if foreignFile.Code != http.StatusOK || foreignFile.Body.String() != "мой текст" {
		t.Fatalf("admin reads personal %d %s", foreignFile.Code, foreignFile.Body.String())
	}
	secondEmployee := doJSON(t, handler, http.MethodPost, "/users", adminToken, map[string]any{
		"login":       "other@corp.local",
		"password":    "other",
		"displayName": "Другой",
		"accessLevel": 1,
	})
	if secondEmployee.Code != http.StatusOK {
		t.Fatalf("second user %d %s", secondEmployee.Code, secondEmployee.Body.String())
	}
	otherToken := login(t, handler, "other@corp.local", "other")
	hiddenFile := doRaw(t, handler, http.MethodGet, "/documents/"+uploaded.DocumentID+"/file", otherToken, nil, "")
	if hiddenFile.Code != http.StatusNotFound {
		t.Fatalf("foreign personal file %d", hiddenFile.Code)
	}
	hiddenDelete := doRaw(t, handler, http.MethodDelete, "/documents/"+uploaded.DocumentID, otherToken, nil, "")
	if hiddenDelete.Code != http.StatusNotFound {
		t.Fatalf("foreign personal delete %d", hiddenDelete.Code)
	}
	globalDelete := doRaw(t, handler, http.MethodDelete, "/documents/"+texts.SeedInstructionID, employeeToken, nil, "")
	if globalDelete.Code != http.StatusForbidden {
		t.Fatalf("employee global delete %d", globalDelete.Code)
	}
	ownDelete := doRaw(t, handler, http.MethodDelete, "/documents/"+uploaded.DocumentID, employeeToken, nil, "")
	if ownDelete.Code != http.StatusNoContent {
		t.Fatalf("own delete %d %s", ownDelete.Code, ownDelete.Body.String())
	}
	if hasDocument(listDocumentPage(t, handler, employeeToken, "").Documents, uploaded.DocumentID) {
		t.Fatal("deleted personal document is still listed")
	}
	adminDelete := doRaw(t, handler, http.MethodDelete, "/documents/"+texts.SeedRestrictedID, adminToken, nil, "")
	if adminDelete.Code != http.StatusNoContent {
		t.Fatalf("admin delete %d", adminDelete.Code)
	}
	if hasDocument(listDocumentPage(t, handler, adminToken, "all").Documents, texts.SeedRestrictedID) {
		t.Fatal("deleted global document is still listed")
	}
}

func must_Handler(t *testing.T, loadedConfig config.Config) http.Handler {
	t.Helper()
	handler, err := NewHandler(loadedConfig)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func login(t *testing.T, handler http.Handler, loginName, password string) string {
	t.Helper()
	response := doJSON(t, handler, http.MethodPost, "/auth/login", "", map[string]string{
		"login":    loginName,
		"password": password,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("login %s status %d body %s", loginName, response.Code, response.Body.String())
	}
	body := decodeJSON[loginResponse](t, response)
	if body.Token == "" {
		t.Fatal("empty token")
	}
	return body.Token
}

func createSession(t *testing.T, handler http.Handler, token string) string {
	t.Helper()
	response := doJSON(t, handler, http.MethodPost, "/sessions", token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("session status %d", response.Code)
	}
	var body struct {
		SessionID string `json:"sessionId"`
	}
	decodeInto(t, response, &body)
	if body.SessionID == "" {
		t.Fatal("empty session")
	}
	return body.SessionID
}

func postMessage(t *testing.T, handler http.Handler, token, sessionID, text string) assistantReply {
	t.Helper()
	response := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", token, map[string]string{"text": text})
	if response.Code != http.StatusOK {
		t.Fatalf("message status %d body %s", response.Code, response.Body.String())
	}
	return decodeJSON[assistantReply](t, response)
}

// Сообщение со сменой режима чата: read или plan. Режим сохраняется в сессии для следующих сообщений
func postModeMessage(t *testing.T, handler http.Handler, token, sessionID, text, mode string) assistantReply {
	t.Helper()
	response := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", token, map[string]string{"text": text, "mode": mode})
	if response.Code != http.StatusOK {
		t.Fatalf("message status %d body %s", response.Code, response.Body.String())
	}
	return decodeJSON[assistantReply](t, response)
}

func listDocuments(t *testing.T, handler http.Handler, token string) []documentResponse {
	t.Helper()
	return listDocumentPage(t, handler, token, "").Documents
}

func listDocumentPage(t *testing.T, handler http.Handler, token, scope string) struct {
	CanUploadGlobal   bool               `json:"canUploadGlobal"`
	CanUploadPersonal bool               `json:"canUploadPersonal"`
	Documents         []documentResponse `json:"documents"`
} {
	t.Helper()
	path := "/documents"
	if scope != "" {
		path += "?scope=" + scope
	}
	response := doJSON(t, handler, http.MethodGet, path, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("documents status %d body %s", response.Code, response.Body.String())
	}
	var body struct {
		CanUploadGlobal   bool               `json:"canUploadGlobal"`
		CanUploadPersonal bool               `json:"canUploadPersonal"`
		Documents         []documentResponse `json:"documents"`
	}
	decodeInto(t, response, &body)
	return body
}

func hasDocument(documents []documentResponse, documentID string) bool {
	for _, document := range documents {
		if document.DocumentID == documentID {
			return true
		}
	}
	return false
}

func TestStubPrepareDocumentListsRequisites(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	sessionID := createSession(t, handler, employeeToken)
	answer := postMessage(t, handler, employeeToken, sessionID, "Как подготовить документ?")
	if answer.Kind != texts.KindFallback || answer.Text != texts.FallbackText || len(answer.Citations) != 0 || answer.Hint != "" {
		t.Fatalf("prepare %+v", answer)
	}
	// «Чтение» бланк не начинает, но подсказывает режим «План»
	read := postMessage(t, handler, employeeToken, sessionID, "Как подготовить модуль распоряжения о создании рабочей группы")
	if read.Kind != texts.KindFallback || read.Hint != texts.ChatModePlanHint {
		t.Fatalf("read mode %+v", read)
	}
	blank := postModeMessage(t, handler, employeeToken, sessionID, "Как подготовить модуль распоряжения о создании рабочей группы", texts.ChatModePlan)
	if blank.Kind != texts.KindTemplateField || blank.Hint != "" {
		t.Fatalf("blank wins %+v", blank)
	}
}

func TestStubContactsFollowQuestion(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	directory := "Центр документационного обеспечения\nтелефон 1-10-20\n\nУправление информационных технологий\nпочта wifi@bank\n"
	uploadDocument(t, handler, employeeToken, "contacts.txt", directory, "Контакты подразделений", "1.0", "1")
	sessionID := createSession(t, handler, employeeToken)

	answer := postMessage(t, handler, employeeToken, sessionID, "Куда писать на wifi")
	if answer.Kind != texts.KindFallback || len(answer.Citations) != 0 || answer.Contacts == nil || len(*answer.Contacts) != 1 || (*answer.Contacts)[0].Department != "Управление информационных технологий" || (*answer.Contacts)[0].Channel != "почта wifi@bank" {
		t.Fatalf("answer contacts %+v", answer.Contacts)
	}
	fallback := postMessage(t, handler, employeeToken, sessionID, "Этого не найдено")
	if fallback.Kind != texts.KindFallback || fallback.Contacts == nil || len(*fallback.Contacts) != 1 || (*fallback.Contacts)[0].Department != "Центр документационного обеспечения" {
		t.Fatalf("fallback contacts %+v", fallback)
	}
	clarifyResponse := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]string{"text": "уточни"})
	if clarifyResponse.Code != http.StatusOK || strings.Contains(clarifyResponse.Body.String(), `"contacts"`) {
		t.Fatalf("clarify body %s", clarifyResponse.Body.String())
	}
}

func TestStubDraftRevision(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	created := decodeJSON[roleResponse](t, doJSON(t, handler, http.MethodPost, "/roles", chiefToken, map[string]any{
		"title": "Только вопросы", "permissions": []string{texts.PermissionChatUse},
	}))
	account := decodeJSON[accountResponse](t, doJSON(t, handler, http.MethodPost, "/users", chiefToken, map[string]any{
		"login": "clerk@corp.local", "password": "clerk-pass", "displayName": "Клерк", "role": texts.RoleUser,
	}))
	if doJSON(t, handler, http.MethodPatch, "/users/"+account.UserID, chiefToken, map[string]any{
		"roleIds": []string{created.RoleID},
	}).Code != http.StatusOK {
		t.Fatal("role was not replaced")
	}
	limitedToken := login(t, handler, "clerk@corp.local", "clerk-pass")
	limitedSession := createSession(t, handler, limitedToken)
	if uploadMultipart(t, handler, "/sessions/"+limitedSession+"/attachments", limitedToken, "draft.txt", "Текст", nil).Code != http.StatusForbidden {
		t.Fatal("upload without drafts.assist")
	}
	if uploadMultipart(t, handler, "/sessions/missing/attachments", employeeToken, "draft.txt", "Текст", nil).Code != http.StatusNotFound {
		t.Fatal("missing session")
	}
	adminSession := createSession(t, handler, chiefToken)
	if uploadMultipart(t, handler, "/sessions/"+adminSession+"/attachments", employeeToken, "draft.txt", "Текст", nil).Code != http.StatusForbidden {
		t.Fatal("foreign session")
	}

	sessionID := createSession(t, handler, employeeToken)
	if uploadMultipart(t, handler, "/sessions/"+sessionID+"/attachments", employeeToken, "sheet.xlsx", "нет", nil).Code != http.StatusBadRequest {
		t.Fatal("xlsx accepted")
	}
	if uploadMultipart(t, handler, "/sessions/"+sessionID+"/attachments", employeeToken, "empty.txt", " \n", nil).Code != http.StatusBadRequest {
		t.Fatal("empty text accepted")
	}
	original := "Черновик отчета."
	uploaded := uploadMultipart(t, handler, "/sessions/"+sessionID+"/attachments", employeeToken, "draft.txt", original, nil)
	if uploaded.Code != http.StatusOK {
		t.Fatalf("upload %d %s", uploaded.Code, uploaded.Body.String())
	}
	attachment := decodeJSON[attachmentResponse](t, uploaded)
	if attachment.AttachmentID != "att-1" || attachment.FileName != "draft.txt" || attachment.Characters != utf8.RuneCountInString(original) {
		t.Fatalf("attachment %+v", attachment)
	}
	if doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]any{
		"text": "допиши", "attachmentIds": []string{"att-missing"},
	}).Code != http.StatusBadRequest {
		t.Fatal("foreign attachment accepted")
	}

	response := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]any{
		"text": "допиши раздел", "attachmentIds": []string{attachment.AttachmentID},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("revision %d %s", response.Code, response.Body.String())
	}
	reply := decodeJSON[assistantReply](t, response)
	if reply.Kind != texts.KindFallback || reply.Text != texts.FallbackText || len(reply.Citations) != 0 || reply.Draft != nil || reply.Contacts == nil || len(*reply.Contacts) != 0 {
		t.Fatalf("reply %+v", reply)
	}

	continued := postMessage(t, handler, employeeToken, sessionID, "сократи")
	if continued.Kind != texts.KindFallback || continued.Draft != nil || len(continued.Citations) != 0 {
		t.Fatalf("continued %+v", continued)
	}

	history := doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, employeeToken, nil)
	var stored struct {
		Messages []struct {
			Role        string              `json:"role"`
			Attachments []messageAttachment `json:"attachments"`
		} `json:"messages"`
	}
	decodeInto(t, history, &stored)
	if len(stored.Messages) < 1 || stored.Messages[0].Role != "user" || len(stored.Messages[0].Attachments) != 1 || stored.Messages[0].Attachments[0].AttachmentID != attachment.AttachmentID || stored.Messages[0].Attachments[0].FileName != "draft.txt" {
		t.Fatalf("history %+v", stored.Messages)
	}
}

func TestStubSessionRenameAndDelete(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	sessionID := createSession(t, handler, employeeToken)
	postMessage(t, handler, employeeToken, sessionID, "Как регистрировать входящий документ")
	foreignID := createSession(t, handler, chiefToken)

	listed := doJSON(t, handler, http.MethodGet, "/sessions", employeeToken, nil)
	var sessionList sessionList
	decodeInto(t, listed, &sessionList)
	if len(sessionList.Sessions) != 1 || !sessionList.Sessions[0].CanRename || !sessionList.Sessions[0].CanDelete || sessionList.Sessions[0].Title == "" {
		t.Fatalf("flags %+v", sessionList.Sessions)
	}
	originalTitle := sessionList.Sessions[0].Title
	if doJSON(t, handler, http.MethodPatch, "/sessions/"+sessionID, employeeToken, map[string]string{"title": "   "}).Code != http.StatusBadRequest {
		t.Fatal("empty title accepted")
	}
	if doJSON(t, handler, http.MethodPatch, "/sessions/"+foreignID, employeeToken, map[string]string{"title": "Чужой"}).Code != http.StatusForbidden {
		t.Fatal("foreign rename")
	}
	if doJSON(t, handler, http.MethodPatch, "/sessions/missing", employeeToken, map[string]string{"title": "Нет"}).Code != http.StatusNotFound {
		t.Fatal("missing rename")
	}
	longTitle := strings.Repeat("а", 80)
	renamed := decodeJSON[sessionLine](t, doJSON(t, handler, http.MethodPatch, "/sessions/"+sessionID, employeeToken, map[string]string{"title": longTitle}))
	if renamed.Title == longTitle || len([]rune(renamed.Title)) > 60 || !renamed.CanDelete || !strings.HasSuffix(renamed.Title, "...") {
		t.Fatalf("renamed %+v", renamed)
	}
	postMessage(t, handler, employeeToken, sessionID, "Следующий вопрос не должен сменить заголовок")
	decodeInto(t, doJSON(t, handler, http.MethodGet, "/sessions", employeeToken, nil), &sessionList)
	if sessionList.Sessions[0].Title != renamed.Title || sessionList.Sessions[0].Title == originalTitle {
		t.Fatalf("title changed %+v", sessionList.Sessions[0])
	}
	if doJSON(t, handler, http.MethodDelete, "/sessions/"+foreignID, employeeToken, nil).Code != http.StatusForbidden {
		t.Fatal("foreign delete")
	}
	if doJSON(t, handler, http.MethodDelete, "/sessions/missing", employeeToken, nil).Code != http.StatusNotFound {
		t.Fatal("missing delete")
	}
	deleted := doJSON(t, handler, http.MethodDelete, "/sessions/"+sessionID, employeeToken, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete %d", deleted.Code)
	}
	if doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, employeeToken, nil).Code != http.StatusNotFound {
		t.Fatal("deleted session still opens")
	}
	decodeInto(t, doJSON(t, handler, http.MethodGet, "/sessions", employeeToken, nil), &sessionList)
	// Флаги рядом со списком: по ним веб показывает меню чата, даже когда чатов не осталось
	if len(sessionList.Sessions) != 0 || !sessionList.CanRename || !sessionList.CanDelete {
		t.Fatalf("list after delete %+v", sessionList)
	}
}

func TestStubDeleteUser(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	sessionID := createSession(t, handler, employeeToken)
	postMessage(t, handler, employeeToken, sessionID, "Как зарегистрировать входящий документ?")
	personal := uploadMultipart(t, handler, "/documents", employeeToken, "mine.txt", "Личная заметка", map[string]string{
		"title": "Личный документ", "scope": texts.ScopePersonal,
	})
	if personal.Code != http.StatusOK {
		t.Fatalf("personal upload %d %s", personal.Code, personal.Body.String())
	}
	personalID := decodeJSON[uploadResponse](t, personal).DocumentID

	if doJSON(t, handler, http.MethodDelete, "/users/"+texts.SeedAdminID, employeeToken, nil).Code != http.StatusForbidden {
		t.Fatal("employee deleted a user")
	}
	if doJSON(t, handler, http.MethodDelete, "/users/"+texts.SeedAdminID, chiefToken, nil).Code != http.StatusForbidden {
		t.Fatal("self delete")
	}
	if doJSON(t, handler, http.MethodDelete, "/users/missing", chiefToken, nil).Code != http.StatusNotFound {
		t.Fatal("missing user")
	}

	// Администратор без roles.manage не удаляет главного администратора
	admin := decodeJSON[accountResponse](t, doJSON(t, handler, http.MethodPost, "/users", chiefToken, map[string]any{
		"login": "admin2@corp.local", "password": "admin2-pass", "displayName": "Второй администратор", "role": texts.RoleUser,
	}))
	if doJSON(t, handler, http.MethodPatch, "/users/"+admin.UserID, chiefToken, map[string]any{"roleId": texts.RoleIDAdmin}).Code != http.StatusOK {
		t.Fatal("admin role")
	}
	adminToken := login(t, handler, "admin2@corp.local", "admin2-pass")
	if doJSON(t, handler, http.MethodDelete, "/users/"+texts.SeedAdminID, adminToken, nil).Code != http.StatusForbidden {
		t.Fatal("admin deleted the chief")
	}

	if deleted := doJSON(t, handler, http.MethodDelete, "/users/"+texts.SeedEmployeeID, adminToken, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", deleted.Code, deleted.Body.String())
	}
	if doJSON(t, handler, http.MethodGet, "/me", employeeToken, nil).Code != http.StatusUnauthorized {
		t.Fatal("token of a deleted user still works")
	}
	if doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, chiefToken, nil).Code != http.StatusNotFound {
		t.Fatal("session of a deleted user remains")
	}
	if hasDocument(listDocumentPage(t, handler, chiefToken, texts.ScopeAll).Documents, personalID) {
		t.Fatal("personal document of a deleted user remains")
	}
	var users struct {
		Users []accountResponse `json:"users"`
	}
	decodeInto(t, doJSON(t, handler, http.MethodGet, "/users", chiefToken, nil), &users)
	for _, account := range users.Users {
		if account.UserID == texts.SeedEmployeeID {
			t.Fatal("deleted user is listed")
		}
	}
	if doJSON(t, handler, http.MethodDelete, "/users/"+admin.UserID, chiefToken, nil).Code != http.StatusNoContent {
		t.Fatal("chief could not delete an admin")
	}
	// Почта освободилась: под ней можно завести новую учетную запись
	recreated := doJSON(t, handler, http.MethodPost, "/users", chiefToken, map[string]any{
		"login": "admin2@corp.local", "password": "new-pass", "displayName": "Новый сотрудник", "role": texts.RoleUser,
	})
	if recreated.Code != http.StatusOK {
		t.Fatalf("recreate %d %s", recreated.Code, recreated.Body.String())
	}
}

func uploadDocument(t *testing.T, handler http.Handler, token, fileName, content, title, edition, accessLevel string) uploadResponse {
	t.Helper()
	response := uploadMultipart(t, handler, "/documents", token, fileName, content, map[string]string{
		"title":       title,
		"edition":     edition,
		"accessLevel": accessLevel,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("upload status %d body %s", response.Code, response.Body.String())
	}
	return decodeJSON[uploadResponse](t, response)
}

func uploadDraft(t *testing.T, handler http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	return uploadMultipart(t, handler, "/drafts/check", token, "draft.txt", "черновик", nil)
}

func uploadMultipart(t *testing.T, handler http.Handler, path, token, fileName, content string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if err = writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return doRaw(t, handler, http.MethodPost, path, token, payload.Bytes(), writer.FormDataContentType())
}

func doJSON(t *testing.T, handler http.Handler, method, path, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	contentType := ""
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		contentType = "application/json"
	}
	return doRaw(t, handler, method, path, token, raw, contentType)
}

func doRaw(t *testing.T, handler http.Handler, method, path, token string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeJSON[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	decodeInto(t, response, &out)
	return out
}

func decodeInto(t *testing.T, response *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}
}

func logWriter() io.Writer {
	return log.Writer()
}

func setLogWriter(writer io.Writer) {
	log.SetOutput(writer)
}

func TestStubProcessesAndFolderImport(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)

	denied := doJSON(t, handler, http.MethodPost, "/processes", employeeToken, map[string]string{"title": "Кадры"})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("employee create %d", denied.Code)
	}
	empty := doJSON(t, handler, http.MethodPost, "/processes", adminToken, map[string]string{"title": " "})
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty title %d", empty.Code)
	}
	created := decodeJSON[processDetail](t, doJSON(t, handler, http.MethodPost, "/processes", adminToken, map[string]string{"title": "Кадры"}))
	if created.ProcessID != "process-1" || created.Title != "Кадры" || created.AnswerPrompt != texts.LiveAnswerPrompt {
		t.Fatalf("created %+v", created)
	}
	listed := decodeJSON[struct {
		Processes []processLine `json:"processes"`
	}](t, doJSON(t, handler, http.MethodGet, "/processes", employeeToken, nil))
	if len(listed.Processes) < 2 || listed.Processes[0].ProcessID == "" {
		t.Fatalf("list %+v", listed.Processes)
	}

	sessionID := createSession(t, handler, employeeToken)
	unknown := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]string{
		"text":      "вопрос",
		"processId": "missing",
	})
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown process %d %s", unknown.Code, unknown.Body.String())
	}

	uploaded := uploadMultipart(t, handler, "/documents", adminToken, "staff.txt", "кадровый регламент", map[string]string{
		"title":   "Кадровый регламент",
		"process": created.ProcessID,
		"scope":   texts.ScopeGlobal,
	})
	if uploaded.Code != http.StatusOK {
		t.Fatalf("upload %d %s", uploaded.Code, uploaded.Body.String())
	}
	staffUpload := decodeJSON[uploadResponse](t, uploaded)
	if staffUpload.ProcessID != created.ProcessID {
		t.Fatalf("upload process %+v", staffUpload)
	}
	page := listDocumentPage(t, handler, adminToken, "")
	filtered := doJSON(t, handler, http.MethodGet, "/documents?process="+created.ProcessID, adminToken, nil)
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), staffUpload.DocumentID) {
		t.Fatalf("filtered %d %s", filtered.Code, filtered.Body.String())
	}
	other := doJSON(t, handler, http.MethodGet, "/documents?process="+texts.ProcessDocuments, adminToken, nil)
	if other.Code != http.StatusOK || strings.Contains(other.Body.String(), staffUpload.DocumentID) {
		t.Fatalf("default process list %s", other.Body.String())
	}
	missingProcess := doJSON(t, handler, http.MethodGet, "/documents?process=missing", adminToken, nil)
	if missingProcess.Code != http.StatusBadRequest {
		t.Fatalf("missing process list %d", missingProcess.Code)
	}
	if len(page.Documents) == 0 || page.Documents[0].ProcessID == "" {
		t.Fatalf("document process %+v", page.Documents)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "imported.txt"), []byte("импорт"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sheet.xlsx"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	imported := postForm(t, handler, "/documents/import", adminToken, map[string]string{
		"path":    dir,
		"process": created.ProcessID,
		"edition": "2.0",
		"scope":   texts.ScopeGlobal,
	})
	if imported.Code != http.StatusOK {
		t.Fatalf("import %d %s", imported.Code, imported.Body.String())
	}
	body := decodeJSON[importResponse](t, imported)
	if len(body.Documents) != 1 || body.Documents[0].ProcessID != created.ProcessID || len(body.Skipped) != 1 || body.Skipped[0] != "sheet.xlsx" {
		t.Fatalf("import body %+v", body)
	}
	parent := postForm(t, handler, "/documents/import", adminToken, map[string]string{"path": filepath.Join(dir, "..", "secret")})
	if parent.Code != http.StatusBadRequest {
		t.Fatalf("parent import %d", parent.Code)
	}
}

func postForm(t *testing.T, handler http.Handler, path, token string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return doRaw(t, handler, http.MethodPost, path, token, payload.Bytes(), writer.FormDataContentType())
}
