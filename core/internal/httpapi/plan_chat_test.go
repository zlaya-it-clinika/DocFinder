package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

// «Чтение» по умолчанию: документ не начинается, «План» без вида документа предлагает список
func TestStubChatModes(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	sessionID := createSession(t, handler, employeeToken)

	read := postMessage(t, handler, employeeToken, sessionID, "Подготовь служебную записку о замене ноутбука")
	if read.Kind != texts.KindFallback || read.Template != nil || read.Hint != texts.ChatModePlanHint {
		t.Fatalf("read mode started a document %+v", read)
	}
	if response := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]string{"text": "привет", "mode": "write"}); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown mode %d", response.Code)
	}

	// В «Плане» вопрос «как составить» тоже начинает документ
	plan := postModeMessage(t, handler, employeeToken, sessionID, "Как составить служебную записку?", texts.ChatModePlan)
	if plan.Kind != texts.KindTemplateField || plan.Template == nil || plan.Template.ID != "service-note" || plan.Hint != "" {
		t.Fatalf("plan layout %+v", plan)
	}
	var history struct {
		Mode string `json:"mode"`
	}
	decodeInto(t, doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, employeeToken, nil), &history)
	if history.Mode != texts.ChatModePlan {
		t.Fatalf("history mode %q", history.Mode)
	}

	// Переход в «Чтение» забывает начатый документ: в «Плане» реплика уже не ответ на вопрос макета
	if reply := postModeMessage(t, handler, employeeToken, sessionID, "Иванову", texts.ChatModeRead); reply.Kind == texts.KindTemplateField {
		t.Fatalf("read continued the layout %+v", reply)
	}
	choice := postModeMessage(t, handler, employeeToken, sessionID, "Надо решить вопрос с ноутбуком", texts.ChatModePlan)
	// Бланки модулей в списке - по названию из свойств файла, без пропусков и номеров примечаний
	if choice.Kind != texts.KindTemplateChoice || !strings.HasPrefix(choice.Text, texts.PlanChoiceText) || !strings.Contains(choice.Text, "1. Служебная записка") ||
		!strings.Contains(choice.Text, "Модуль распоряжения о создании рабочей группы") || strings.Contains(choice.Text, "[") {
		t.Fatalf("choice %+v", choice)
	}
	// Непонятный ответ - список еще раз, номер - документ по исходной просьбе
	retry := postMessage(t, handler, employeeToken, sessionID, "не знаю")
	if retry.Kind != texts.KindTemplateChoice || !strings.HasPrefix(retry.Text, texts.PlanChoiceRetryText) {
		t.Fatalf("retry %+v", retry)
	}
	picked := postMessage(t, handler, employeeToken, sessionID, "1")
	if picked.Kind != texts.KindTemplateField || picked.Template == nil || picked.Template.ID != "service-note" {
		t.Fatalf("picked %+v", picked)
	}
	if cancelled := postMessage(t, handler, employeeToken, sessionID, "отмена"); cancelled.Text != texts.LayoutCancelledText {
		t.Fatalf("cancel %+v", cancelled)
	}
	// Название в ответ на список
	postMessage(t, handler, employeeToken, sessionID, "Надо решить вопрос с ноутбуком")
	if letter := postMessage(t, handler, employeeToken, sessionID, "письмо"); letter.Template == nil || letter.Template.ID != "letter" {
		t.Fatalf("letter by name %+v", letter)
	}
}

// Модель выбирает вид документа: ровно один код из списка, иначе - список на выбор
func TestClassify_PlanDocument(t *testing.T) {
	documents := list_PlanDocuments()
	chat := &scriptedChat{replies: []string{"letter", "Код: act.", "НЕТ", "letter или act", "contract"}}
	for _, want := range []string{"letter", "act", "", "", ""} {
		document, found := classify_PlanDocument(chat, documents, "надо попросить поставщика прислать счет")
		if found != (want != "") || document.ID != want {
			t.Fatalf("want %q got %q %v", want, document.ID, found)
		}
	}
	if !strings.Contains(chat.systems[0], "НЕТ") || !strings.Contains(chat.users[0], "service-note - Служебная записка") {
		t.Fatalf("prompt %q %q", chat.systems[0], chat.users[0])
	}
	if _, found := classify_PlanDocument(nil, documents, "любая просьба"); found {
		t.Fatal("no model, no choice")
	}
}

// Вопросы документа одной карточкой: ядро отдает все поля, ответы приходят пачкой,
// в истории реплика сотрудника хранит подписи полей. Выбор документа - кнопкой с названием
func TestStubDocumentQuestionsInOneTurn(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	sessionID := createSession(t, handler, employeeToken)

	choice := postModeMessage(t, handler, employeeToken, sessionID, "Надо решить вопрос с ноутбуком", texts.ChatModePlan)
	if choice.Kind != texts.KindTemplateChoice || len(choice.Options) < 8 || choice.Options[0] != "Служебная записка" {
		t.Fatalf("choice options %+v", choice.Options)
	}
	started := postMessage(t, handler, employeeToken, sessionID, "Деловое письмо")
	if started.Template == nil || started.Template.ID != "letter" || len(started.Template.Questions) != 6 || started.Template.Questions[0].Field != texts.LayoutFieldAddressee {
		t.Fatalf("letter questions %+v", started.Template)
	}

	send := func(text string, answers []map[string]string) assistantReply {
		response := doJSON(t, handler, http.MethodPost, "/sessions/"+sessionID+"/messages", employeeToken, map[string]any{"text": text, "answers": answers})
		if response.Code != http.StatusOK {
			t.Fatalf("answers %d %s", response.Code, response.Body.String())
		}
		return decodeJSON[assistantReply](t, response)
	}
	partial := send("Адресат: АО Профиль", []map[string]string{
		{"field": texts.LayoutFieldAddressee, "value": "АО \"Профиль\", генеральный директор Сидоров П.П."},
		{"field": texts.LayoutFieldSubject, "value": "о предоставлении данных"},
		{"field": texts.LayoutFieldPoints, "value": ""},
		{"field": texts.LayoutFieldSigner, "value": "Директор Петров А.Б."},
		{"field": texts.LayoutFieldAttach, "value": "нет"},
	})
	if partial.Kind != texts.KindTemplateField || partial.Template == nil || len(partial.Template.Questions) != 2 || partial.Template.Questions[0].Field != texts.LayoutFieldPoints {
		t.Fatalf("remaining questions %+v", partial.Template)
	}
	ready := send("Суть: просим данные", []map[string]string{
		{"field": texts.LayoutFieldPoints, "value": "Просим направить данные до 01.09.2026."},
		{"field": texts.LayoutFieldExecutor, "value": "нет"},
	})
	if ready.Kind != texts.KindTemplateReady || ready.Template == nil || ready.Template.DraftLink == "" {
		t.Fatalf("ready %+v", ready)
	}

	var history struct {
		Messages []json.RawMessage `json:"messages"`
	}
	decodeInto(t, doJSON(t, handler, http.MethodGet, "/sessions/"+sessionID, employeeToken, nil), &history)
	var answered struct {
		Text    string       `json:"text"`
		Answers []answerLine `json:"answers"`
	}
	if err := json.Unmarshal(history.Messages[4], &answered); err != nil {
		t.Fatal(err)
	}
	if answered.Text != "Адресат: АО Профиль" || len(answered.Answers) != 5 || answered.Answers[0].Label != "Адресат" || answered.Answers[3].Label != "Подписант" {
		t.Fatalf("stored answers %+v", answered)
	}
}
