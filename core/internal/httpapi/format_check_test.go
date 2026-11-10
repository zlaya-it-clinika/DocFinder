package httpapi

import (
	"archive/zip"
	"bytes"
	"net/http"
	"strings"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func build_TestDocx(t *testing.T, document string) string {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	writer, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(document)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}

func TestStubDraftCheckFormatting(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	// Внешний документ: Arial, поля 20 мм, текст без приложения и подписи
	docx := build_TestDocx(t, `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`+
		`<w:p><w:r><w:rPr><w:rFonts w:ascii="Arial"/></w:rPr><w:t>Прошу выделить новый ноутбук.</w:t></w:r></w:p>`+
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1134"/></w:sectPr></w:body></w:document>`)
	response := uploadMultipart(t, handler, "/drafts/check", employeeToken, "external.docx", docx, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("check %d %s", response.Code, response.Body.String())
	}
	var checked draftCheckResponse
	decodeInto(t, response, &checked)
	if !checked.FormatChecked || len(checked.Recommendations) == 0 {
		t.Fatalf("docx check %+v", checked)
	}
	margin := checked.Recommendations[0]
	if margin.Level != texts.FormatLevelError || !strings.Contains(margin.Text, "Левое поле 20 мм") ||
		margin.Citation.Paragraph != "8.16" || margin.Citation.OriginalLink != "/documents/"+texts.SeedInstructionID+"/file#p8.16" {
		t.Fatalf("margin %+v", margin)
	}
	hasFont, hasText := false, false
	for _, item := range checked.Recommendations {
		hasFont = hasFont || strings.Contains(item.Text, "Шрифт Arial")
		// Рекомендации по тексту идут после оформления и без уровня
		hasText = hasText || (item.Level == "" && item.Text == texts.DraftRecommendation)
	}
	if !hasFont || !hasText {
		t.Fatalf("recommendations %+v", checked.Recommendations)
	}

	plain := uploadMultipart(t, handler, "/drafts/check", employeeToken, "ready.txt", "Приложение № 1. Подпись Иванова.", nil)
	var plainChecked draftCheckResponse
	decodeInto(t, plain, &plainChecked)
	if plainChecked.FormatChecked || len(plainChecked.Recommendations) != 0 {
		t.Fatalf("txt check %+v", plainChecked)
	}
}

func TestStubDraftFixFormatting(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	docx := build_TestDocx(t, `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`+
		`<w:p><w:r><w:rPr><w:rFonts w:ascii="Arial"/></w:rPr><w:t>Прошу выделить новый ноутбук до 01.11.2026.</w:t></w:r></w:p>`+
		`<w:p><w:r><w:rPr><w:color w:val="FF0000"/></w:rPr><w:t>[1] заполнить</w:t></w:r></w:p>`+
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1134"/></w:sectPr></w:body></w:document>`)
	adminToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	created := doJSON(t, handler, http.MethodPost, "/organization/officials", adminToken, map[string]any{"position": "Генеральный директор", "fullName": "Петров Андрей Борисович", "chief": true})
	if created.Code != http.StatusOK {
		t.Fatalf("official %d", created.Code)
	}
	official := decodeJSON[struct {
		OfficialID string `json:"officialId"`
	}](t, created)
	// Проверка говорит, каких реквизитов нет, и предлагает должностных лиц
	checkResponse := uploadMultipart(t, handler, "/drafts/check", employeeToken, "external.docx", docx, nil)
	var checked0 draftCheckResponse
	decodeInto(t, checkResponse, &checked0)
	if checked0.Requisites == nil || strings.Join(checked0.Requisites.Missing, ",") != "addressee,title,signature" ||
		len(checked0.Requisites.Officials) != 1 || !checked0.Requisites.Officials[0].Chief {
		t.Fatalf("requisites %+v", checked0.Requisites)
	}
	response := uploadMultipart(t, handler, "/drafts/fix", employeeToken, "external.docx", docx, map[string]string{
		"signerId": official.OfficialID,
		"title":    "о замене ноутбука",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("fix %d %s", response.Code, response.Body.String())
	}
	var fixed draftFixResponse
	decodeInto(t, response, &fixed)
	if fixed.FileName != "external"+texts.FormatFixFileSuffix || fixed.DraftLink == "" || len(fixed.Applied) < 3 {
		t.Fatalf("fixed %+v", fixed)
	}
	if !strings.Contains(strings.Join(fixed.Applied, ","), texts.FormatFixSignature) || !strings.Contains(strings.Join(fixed.Applied, ","), texts.FormatFixTitle) {
		t.Fatalf("requisites not added %+v", fixed.Applied)
	}
	// Красный текст и номер поля - содержание, они остаются в замечаниях
	for _, item := range fixed.Recommendations {
		if !strings.Contains(item.Citation.Document, texts.FormatTemplateProfile) {
			t.Fatalf("left after fix %+v", item)
		}
	}
	file := doRaw(t, handler, http.MethodGet, fixed.DraftLink, employeeToken, nil, "")
	if file.Code != http.StatusOK {
		t.Fatalf("download %d", file.Code)
	}
	recheck := uploadMultipart(t, handler, "/drafts/check", employeeToken, "fixed.docx", file.Body.String(), nil)
	var checked draftCheckResponse
	decodeInto(t, recheck, &checked)
	for _, item := range checked.Recommendations {
		if item.Level != "" && !strings.Contains(item.Citation.Document, texts.FormatTemplateProfile) {
			t.Fatalf("not fixed %+v", item)
		}
	}
	if plain := uploadMultipart(t, handler, "/drafts/fix", employeeToken, "ready.txt", "текст", nil); plain.Code != http.StatusBadRequest {
		t.Fatalf("txt fix %d", plain.Code)
	}
}
