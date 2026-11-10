package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/config"
	"docfinder/core/internal/layout"
	"docfinder/core/internal/texts"
)

type processCards struct {
	CanManage bool          `json:"canManage"`
	Processes []processLine `json:"processes"`
}

func TestStubProcessCardsAndChatSelection(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)

	// Карточка с описанием и цветом. Неизвестный цвет - 400
	if bad := doJSON(t, handler, http.MethodPost, "/processes", adminToken, map[string]string{"title": "Кадры", "color": "pink"}); bad.Code != http.StatusBadRequest {
		t.Fatalf("bad color %d", bad.Code)
	}
	created := decodeJSON[processDetail](t, doJSON(t, handler, http.MethodPost, "/processes", adminToken, map[string]string{
		"title": "Отдел продаж", "description": "Скидки и договоры", "color": "green",
	}))
	if created.Color != "green" || created.Description != "Скидки и договоры" {
		t.Fatalf("created %+v", created)
	}

	// Сотрудник видит карточки без подсказок модели, администратор - с ними
	employeeCards := decodeJSON[processCards](t, doJSON(t, handler, http.MethodGet, "/processes", employeeToken, nil))
	if employeeCards.CanManage || len(employeeCards.Processes) != 2 || employeeCards.Processes[0].AnswerPrompt != nil {
		t.Fatalf("employee cards %+v", employeeCards)
	}
	adminCards := decodeJSON[processCards](t, doJSON(t, handler, http.MethodGet, "/processes", adminToken, nil))
	if !adminCards.CanManage || adminCards.Processes[0].AnswerPrompt == nil || adminCards.Processes[0].ProcessID != texts.ProcessDocuments || adminCards.Processes[0].DocumentCount == 0 {
		t.Fatalf("admin cards %+v", adminCards)
	}

	// Правка: сотруднику нельзя, пустая подсказка возвращает стандартную
	if denied := doJSON(t, handler, http.MethodPatch, "/processes/"+created.ProcessID, employeeToken, map[string]string{"title": "x"}); denied.Code != http.StatusForbidden {
		t.Fatalf("employee patch %d", denied.Code)
	}
	patched := decodeJSON[processLine](t, doJSON(t, handler, http.MethodPatch, "/processes/"+created.ProcessID, adminToken, map[string]string{
		"title": "Продажи", "answerPrompt": "Ты консультант отдела продаж", "selectPrompt": " ",
	}))
	if patched.Title != "Продажи" || patched.AnswerPrompt == nil || *patched.AnswerPrompt != "Ты консультант отдела продаж" || *patched.SelectPrompt != texts.LiveSelectPrompt {
		t.Fatalf("patched %+v", patched)
	}

	// Выбор процесса запоминается в чате. Ответ по инструкции документооборота в продажах не находится
	sessionID := createSession(t, handler, employeeToken)
	sales := decodeJSON[assistantReply](t, doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]string{
		"text": "Как зарегистрировать входящий документ?", "processId": created.ProcessID,
	}))
	if sales.Kind != texts.KindFallback || sales.Process == nil || sales.Process.Title != "Продажи" {
		t.Fatalf("sales %+v", sales)
	}
	history := doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, employeeToken, nil)
	var stored struct {
		ProcessID string `json:"processId"`
	}
	decodeInto(t, history, &stored)
	if stored.ProcessID != created.ProcessID {
		t.Fatalf("stored process %+v", stored)
	}
	kept := postMessage(t, handler, employeeToken, sessionID, "Как зарегистрировать входящий документ?")
	if kept.Kind != texts.KindFallback || kept.Process == nil {
		t.Fatalf("selection not kept %+v", kept)
	}

	// Документооборот: ответ с меткой темы и процессом у источника
	documents := decodeJSON[assistantReply](t, doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]string{
		"text": "Как зарегистрировать входящий документ?", "processId": texts.ProcessDocuments,
	}))
	if documents.Process == nil || documents.Process.ProcessID != texts.ProcessDocuments {
		t.Fatalf("documents %+v", documents)
	}

	// Все процессы: пустая строка, метки темы нет
	all := decodeJSON[assistantReply](t, doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]string{
		"text": "Как зарегистрировать входящий документ?", "processId": "",
	}))
	if all.Process != nil {
		t.Fatalf("all %+v", all)
	}

	// Удаление: документооборот удалить нельзя, документы удалённого процесса переезжают в документооборот,
	// чат с удалённым процессом снова ищет по всем
	if protected := doJSON(t, handler, http.MethodDelete, "/processes/"+texts.ProcessDocuments, adminToken, nil); protected.Code != http.StatusForbidden {
		t.Fatalf("delete documents %d", protected.Code)
	}
	upload := uploadMultipart(t, handler, "/documents", adminToken, "sales.txt", "скидки", map[string]string{"title": "Регламент скидок", "process": created.ProcessID, "scope": texts.ScopeGlobal})
	uploaded := decodeJSON[uploadResponse](t, upload)
	decodeJSON[assistantReply](t, doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]string{
		"text": "вопрос", "processId": created.ProcessID,
	}))
	if deleted := doJSON(t, handler, http.MethodDelete, "/processes/"+created.ProcessID, adminToken, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete %d", deleted.Code)
	}
	moved := doJSON(t, handler, http.MethodGet, "/documents?process="+texts.ProcessDocuments, adminToken, nil)
	if !strings.Contains(moved.Body.String(), uploaded.DocumentID) {
		t.Fatalf("document not moved %s", moved.Body.String())
	}
	decodeInto(t, doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, employeeToken, nil), &stored)
	if stored.ProcessID != "" {
		t.Fatalf("deleted process kept in session %+v", stored)
	}
}

func TestStubProcessMarksCitations(t *testing.T) {
	layoutFormat, err := layout.Load_Format(nil)
	if err != nil {
		t.Fatal(err)
	}
	stubStore := create_StubStore(false, auth.Settings{}, layoutFormat)
	answer := assistantReply{Kind: texts.KindAnswer, Citations: []citation{{DocumentID: texts.SeedInstructionID}}}
	marked := stubStore.apply_StubProcess(answer, texts.ProcessDocuments)
	if marked.Kind != texts.KindAnswer || marked.Citations[0].ProcessID != texts.ProcessDocuments || marked.Process == nil {
		t.Fatalf("marked %+v", marked)
	}
	stubStore.store_Process(&processRecord{id: "process-sales", title: "Продажи"})
	other := stubStore.apply_StubProcess(answer, "process-sales")
	if other.Kind != texts.KindFallback || other.Process == nil || other.Process.Title != "Продажи" {
		t.Fatalf("other process %+v", other)
	}
}

func TestProcessBriefAndPrompts(t *testing.T) {
	brief := format_ProcessBrief("Отдел продаж", "Скидки", []string{"Регламент скидок", "Договор"})
	for _, part := range []string{texts.LiveProcessLabel, "Отдел продаж", "Скидки", "Регламент скидок; Договор"} {
		if !strings.Contains(brief, part) {
			t.Fatalf("brief %q misses %q", brief, part)
		}
	}
	prompts := replyPrompts{Select: "s", Answer: "a", Clarify: "c"}
	if prompts.with_Brief("") != prompts {
		t.Fatal("empty brief changed prompts")
	}
	withBrief := prompts.with_Brief(brief)
	if !strings.HasPrefix(withBrief.Answer, "a\n\n") || !strings.Contains(withBrief.Select, "Отдел продаж") || !strings.Contains(withBrief.Clarify, "Договор") {
		t.Fatalf("prompts %+v", withBrief)
	}
}
