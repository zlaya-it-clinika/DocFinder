package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func TestMatch_DocumentOverview(t *testing.T) {
	documents := []overviewCandidate{
		{DocumentID: "doc-1", Title: "Инструкция по ДОУ учебный пример"},
		{DocumentID: "doc-2", Title: "Матрица обязательного визирования внутренних и внешних документов"},
		{DocumentID: "doc-3", Title: "Федеральный закон О коммерческой тайне"},
		{DocumentID: "doc-4", Title: "Справочник подразделений и контакты"},
		{DocumentID: "doc-5", Title: "Об утверждении федерального стандарта бухгалтерского учета"},
	}
	short := []overviewCandidate{{DocumentID: "doc-16", Title: "О коммерческой тайне"}}
	if document, found := match_DocumentOverview("Закон о коммерческой тайне", short); !found || document.DocumentID != "doc-16" {
		t.Fatalf("short title %q %t", document.DocumentID, found)
	}
	cases := []struct {
		question string
		want     string
	}{
		{"Закон о коммерческой тайне", "doc-3"},
		{"Что такое закон о коммерческой тайне?", "doc-3"},
		{"Дай скачать федеральный закон о коммерческой тайне", "doc-3"},
		{"Матрица обязательного визирования", "doc-2"},
		{"Расскажи про матрицу визирования документов", "doc-2"},
		{"Инструкция по ДОУ", "doc-1"},
		// Вопросы по содержанию документа - обычный поиск
		{"Кто визирует письмо в иностранный центробанк?", ""},
		{"Какой срок хранения коммерческой тайны?", ""},
		{"Когда регистрируются входящие документы?", ""},
		{"Инструкция", ""},
		{"Федеральный стандарт бухгалтерского учета", "doc-5"},
		{"Приказ о коммерческой тайне", "doc-3"},
		// Справочник контактов справкой не отвечает
		{"Справочник подразделений", ""},
	}
	for _, testCase := range cases {
		document, found := match_DocumentOverview(testCase.question, documents)
		if (testCase.want == "") == found || (found && document.DocumentID != testCase.want) {
			t.Errorf("%q: got %q %t, want %q", testCase.question, document.DocumentID, found, testCase.want)
		}
	}
}

func TestStubDocumentOverviewReply(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	uploaded := uploadMultipart(t, handler, "/documents", adminToken, "secret.txt", "Глава 1. Общие положения. Закон регулирует отношения, связанные с коммерческой тайной.", map[string]string{
		"title": "Федеральный закон О коммерческой тайне",
		"scope": texts.ScopeGlobal,
	})
	if uploaded.Code != http.StatusOK {
		t.Fatalf("upload %d %s", uploaded.Code, uploaded.Body.String())
	}
	var created uploadResponse
	if err := json.Unmarshal(uploaded.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	sessionID := createSession(t, handler, employeeToken)
	reply := postMessage(t, handler, employeeToken, sessionID, "Закон о коммерческой тайне")
	if reply.Kind != texts.KindAnswer || reply.Download == nil || reply.Download.Link != "/documents/"+created.DocumentID+"/file" || len(reply.Citations) != 1 || !strings.Contains(reply.Text, "Федеральный закон О коммерческой тайне") {
		t.Fatalf("overview %+v", reply)
	}
	if file := doRaw(t, handler, http.MethodGet, reply.Download.Link, employeeToken, nil, ""); file.Code != http.StatusOK {
		t.Fatalf("download %d", file.Code)
	}
	if question := postMessage(t, handler, employeeToken, sessionID, "Когда регистрируют входящий документ?"); question.Download != nil {
		t.Fatalf("question got overview %+v", question)
	}
	// Закрытый документ сотруднику справкой не показывается
	if closed := postMessage(t, handler, employeeToken, sessionID, texts.SeedRestrictedTitle); closed.Download != nil {
		t.Fatalf("closed document overview %+v", closed)
	}
}

func TestStubEditionsArchiveAndStandardDocument(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	newEdition := map[string]string{"title": texts.SeedInstructionTitle, "scope": texts.ScopeGlobal, "edition": "2.0"}
	for _, body := range []string{"Инструкция, редакция 2.0", "Инструкция, редакция 2.0"} {
		if response := uploadMultipart(t, handler, "/documents", adminToken, "instruction-2.txt", body, newEdition); response.Code != http.StatusOK {
			t.Fatalf("new edition %d", response.Code)
		}
	}
	// Тот же файл второй раз архив не пополняет
	var seed documentResponse
	for _, document := range listDocumentPage(t, handler, employeeToken, "").Documents {
		if document.DocumentID == texts.SeedInstructionID {
			seed = document
		}
	}
	if seed.Edition != "2.0" || seed.ArchivedEditions != 1 || !seed.Standard {
		t.Fatalf("seed after new edition %+v", seed)
	}
	listed := doJSON(t, handler, http.MethodGet, "/documents/"+texts.SeedInstructionID+"/editions", employeeToken, nil)
	var editions editionsResponse
	if err := json.Unmarshal(listed.Body.Bytes(), &editions); err != nil || listed.Code != http.StatusOK || len(editions.Editions) != 1 || editions.Editions[0].Edition != texts.EditionDefault {
		t.Fatalf("editions %d %s", listed.Code, listed.Body.String())
	}
	old := doRaw(t, handler, http.MethodGet, editions.Editions[0].FileLink, employeeToken, nil, "")
	if old.Code != http.StatusOK || old.Body.String() != texts.SeedInstructionBody {
		t.Fatalf("archived file %d %q", old.Code, old.Body.String())
	}
	if missing := doRaw(t, handler, http.MethodGet, "/documents/"+texts.SeedInstructionID+"/editions/999/file", employeeToken, nil, ""); missing.Code != http.StatusNotFound {
		t.Fatalf("missing edition %d", missing.Code)
	}
	// Архив закрытого документа сотруднику не виден
	if closed := doRaw(t, handler, http.MethodGet, "/documents/"+texts.SeedRestrictedID+"/editions", employeeToken, nil, ""); closed.Code != http.StatusForbidden {
		t.Fatalf("closed editions %d", closed.Code)
	}
	deleted := doRaw(t, handler, http.MethodDelete, "/documents/"+texts.SeedInstructionID, adminToken, nil, "")
	if deleted.Code != http.StatusConflict || !strings.Contains(deleted.Body.String(), texts.ErrorStandardDocument) {
		t.Fatalf("standard delete %d %s", deleted.Code, deleted.Body.String())
	}
}
