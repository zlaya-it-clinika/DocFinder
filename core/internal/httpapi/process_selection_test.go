package httpapi

import (
	"net/http"
	"reflect"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func TestSplit_ProcessSelection(t *testing.T) {
	if got := split_ProcessSelection(" documents, process-1,,documents "); !reflect.DeepEqual(got, []string{"documents", "process-1"}) {
		t.Fatalf("split %v", got)
	}
	if got := join_ProcessSelection([]string{"process-1", " documents", "process-1"}); got != "process-1,documents" {
		t.Fatalf("join %q", got)
	}
	if !check_ProcessSelected("", "process-2") || !check_ProcessSelected("documents,process-2", "process-2") || check_ProcessSelected("documents", "process-2") {
		t.Fatal("selected")
	}
}

func TestStubSeveralProcessesInChat(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	finance := decodeJSON[processDetail](t, doJSON(t, handler, http.MethodPost, "/processes", adminToken, map[string]string{
		"title": "Финансы",
		"color": "green",
	}))
	sessionID := createSession(t, handler, adminToken)
	// Документооборот и Финансы сразу: справка по инструкции документооборота есть, отметка - оба процесса
	both := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", adminToken, map[string]any{
		"text":       texts.SeedInstructionTitle,
		"processIds": []string{texts.ProcessDocuments, finance.ProcessID},
	})
	reply := decodeJSON[assistantReply](t, both)
	if both.Code != http.StatusOK || reply.Kind != texts.KindAnswer || reply.Process == nil || !reflect.DeepEqual(reply.Process.ProcessIDs, []string{texts.ProcessDocuments, finance.ProcessID}) {
		t.Fatalf("both processes %d %+v", both.Code, reply)
	}
	session := decodeJSON[struct {
		ProcessIDs []string `json:"processIds"`
	}](t, doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, adminToken, nil))
	if !reflect.DeepEqual(session.ProcessIDs, []string{texts.ProcessDocuments, finance.ProcessID}) {
		t.Fatalf("saved selection %v", session.ProcessIDs)
	}
	// Только Финансы: инструкции документооборота там нет - справки нет, отказ
	if only := postMessage(t, handler, adminToken, createSessionWithProcess(t, handler, adminToken, finance.ProcessID), texts.SeedInstructionTitle); only.Kind != texts.KindFallback {
		t.Fatalf("finance only %+v", only)
	}
	// Неизвестный процесс в выборе - ошибка запроса
	unknown := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", adminToken, map[string]any{
		"text":       "Вопрос",
		"processIds": []string{texts.ProcessDocuments, "process-404"},
	})
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown process %d", unknown.Code)
	}
	// Удаленный процесс выпадает из выбора, остальные остаются
	if deleted := doRaw(t, handler, http.MethodDelete, "/processes/"+finance.ProcessID, adminToken, nil, ""); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete process %d", deleted.Code)
	}
	session = decodeJSON[struct {
		ProcessIDs []string `json:"processIds"`
	}](t, doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, adminToken, nil))
	if !reflect.DeepEqual(session.ProcessIDs, []string{texts.ProcessDocuments}) {
		t.Fatalf("selection after delete %v", session.ProcessIDs)
	}
}

func createSessionWithProcess(t *testing.T, handler http.Handler, token, processID string) string {
	t.Helper()
	sessionID := createSession(t, handler, token)
	response := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", token, map[string]any{"text": "Привет", "processIds": []string{processID}})
	if response.Code != http.StatusOK {
		t.Fatalf("select process %d", response.Code)
	}
	return sessionID
}
