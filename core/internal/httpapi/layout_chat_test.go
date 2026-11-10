package httpapi

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func TestLayoutDialogInStubChat(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	for _, body := range []map[string]any{
		{"position": "Генеральный директор", "fullName": "Петров Андрей Борисович", "chief": true},
		{"position": "Руководитель ИТ-отдела", "fullName": "Иванов И.И."},
	} {
		if response := doJSON(t, handler, http.MethodPost, "/organization/officials", adminToken, body); response.Code != http.StatusOK {
			t.Fatalf("official %d", response.Code)
		}
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 20, 20))); err != nil {
		t.Fatal(err)
	}
	if response := uploadMultipart(t, handler, "/organization/images/emblem", adminToken, "emblem.png", picture.String(), nil); response.Code != http.StatusOK {
		t.Fatalf("emblem %d", response.Code)
	}

	sessionID := createSession(t, handler, employeeToken)
	// Вопрос «как составить» - к базе знаний, а не заказ документа
	if question := postMessage(t, handler, employeeToken, sessionID, "Как составить служебную записку?"); question.Kind == texts.KindTemplateField {
		t.Fatalf("question started a layout %+v", question)
	}
	first := postModeMessage(t, handler, employeeToken, sessionID, "Подготовь служебную записку о замене ноутбука", texts.ChatModePlan)
	if first.Kind != texts.KindTemplateField || first.Template == nil || first.Template.Field != texts.LayoutFieldAddressee || !strings.Contains(first.Text, "2. Руководитель ИТ-отдела Иванов И.И.") {
		t.Fatalf("first %+v", first)
	}
	for _, answer := range []string{"2", "менеджер отдела продаж Петрова Мария Сергеевна", "Ноутбук сломался 10.08.2026, прошу выдать новый.", "нет"} {
		first = postMessage(t, handler, employeeToken, sessionID, answer)
	}
	if first.Kind != texts.KindTemplateReady || first.Template == nil || first.Template.DraftLink == "" || !strings.Contains(first.Template.DraftText, "СЛУЖЕБНАЯ ЗАПИСКА") {
		t.Fatalf("ready %+v", first)
	}
	file := doRaw(t, handler, http.MethodGet, first.Template.DraftLink, employeeToken, nil, "")
	if file.Code != http.StatusOK || !strings.Contains(file.Header().Get("Content-Disposition"), "docx") {
		t.Fatalf("draft file %d %q", file.Code, file.Header().Get("Content-Disposition"))
	}
	archive, err := zip.NewReader(bytes.NewReader(file.Body.Bytes()), int64(file.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var documentXML string
	for _, entry := range archive.File {
		if entry.Name == "word/document.xml" {
			reader, _ := entry.Open()
			raw, _ := io.ReadAll(reader)
			reader.Close()
			documentXML = string(raw)
		}
	}
	// ФИО по Инструкции по ДОУ (п. 8.12.1, 8.19.2): инициалы перед фамилией, между инициалами пробел
	if !strings.Contains(documentXML, "И. И. Иванов") || !strings.Contains(documentXML, "М. С. Петрова") {
		t.Fatal("document has no addressee or signature")
	}

	// Письмо: герб из вкладки «Организация», подписант по умолчанию - генеральный директор, отмена посреди диалога
	letter := postMessage(t, handler, employeeToken, sessionID, "Напиши письмо о предоставлении данных")
	if letter.Kind != texts.KindTemplateField {
		t.Fatalf("letter %+v", letter)
	}
	if cancelled := postMessage(t, handler, employeeToken, sessionID, "отмена"); cancelled.Text != texts.LayoutCancelledText {
		t.Fatalf("cancel %+v", cancelled)
	}
	if after := postMessage(t, handler, employeeToken, sessionID, "Иванову"); after.Kind == texts.KindTemplateField {
		t.Fatalf("dialog continued after cancel %+v", after)
	}
}

func TestBuild_AttachmentHeaderEncodesCyrillic(t *testing.T) {
	header := build_AttachmentHeader("Служебная записка.docx")
	if !strings.Contains(header, `filename="_________ _______.docx"`) || !strings.Contains(header, "filename*=UTF-8''%D0%A1%D0%BB") {
		t.Fatalf("header %q", header)
	}
	if plain := build_AttachmentHeader("order.docx"); plain != `attachment; filename="order.docx"` {
		t.Fatalf("plain %q", plain)
	}
}
