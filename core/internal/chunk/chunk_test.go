package chunk

import (
	"archive/zip"
	"bytes"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSplit_DocumentTextUsesHeadings(t *testing.T) {
	raw := strings.Join([]string{
		"Раздел 1",
		"Общие положения",
		"Пункт 3.2 Текст пункта",
		"Приложение А",
		"Форма бланка",
	}, "\n")
	chunks := Split_DocumentText(raw)
	if len(chunks) != 3 {
		t.Fatalf("chunks %d %+v", len(chunks), chunks)
	}
	if chunks[0].Section != "1" || chunks[0].Paragraph != "" || chunks[0].Content != "Общие положения" {
		t.Fatalf("section %+v", chunks[0])
	}
	if chunks[1].Section != "1" || chunks[1].Paragraph != "3.2" || chunks[1].Content != "Текст пункта" {
		t.Fatalf("paragraph %+v", chunks[1])
	}
	if chunks[2].Appendix != "А" || chunks[2].Section != "" || chunks[2].Content != "Форма бланка" {
		t.Fatalf("appendix %+v", chunks[2])
	}
	numbered := Split_DocumentText("3.2 Регистрация\nДальше")
	if len(numbered) != 1 || numbered[0].Paragraph != "3.2" || numbered[0].Content != "Регистрация\nДальше" {
		t.Fatalf("numbered %+v", numbered)
	}
}

func TestSplit_DocumentTextFixedLengthWithoutHeadings(t *testing.T) {
	raw := strings.Repeat("а", chunkRuneLimit+1)
	chunks := Split_DocumentText(raw)
	if len(chunks) != 2 {
		t.Fatalf("chunks %d", len(chunks))
	}
	if runeCount(chunks[0].Content) != chunkRuneLimit || runeCount(chunks[1].Content) != 1 {
		t.Fatalf("sizes %d %d", runeCount(chunks[0].Content), runeCount(chunks[1].Content))
	}
	if chunks[0].Section != "" || chunks[0].Paragraph != "" || chunks[0].Appendix != "" {
		t.Fatalf("metadata %+v", chunks[0])
	}
}

func TestExtract_DocumentTextTxtAndDocx(t *testing.T) {
	plain, err := Extract_DocumentText("note.txt", []byte("Пункт 1.1 Строка"))
	if err != nil || !strings.Contains(plain, "Пункт 1.1") {
		t.Fatalf("txt %v %s", err, plain)
	}
	docxBody := build_Docx(t, "Раздел 4", "Содержание раздела")
	docxText, err := Extract_DocumentText("blank.docx", docxBody)
	if err != nil {
		t.Fatal(err)
	}
	chunks := Split_DocumentText(docxText)
	if len(chunks) != 1 || chunks[0].Section != "4" || chunks[0].Content != "Содержание раздела" {
		t.Fatalf("docx %+v text %q", chunks, docxText)
	}
	if _, err := Extract_DocumentText("sheet.xlsx", []byte("nope")); err != ErrUnsupportedFile {
		t.Fatalf("xlsx error %v", err)
	}
}

func build_Docx(t *testing.T, lines ...string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	var document strings.Builder
	document.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	document.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, line := range lines {
		document.WriteString(`<w:p><w:r><w:t>`)
		document.WriteString(line)
		document.WriteString(`</w:t></w:r></w:p>`)
	}
	document.WriteString(`</w:body></w:document>`)
	if _, err := file.Write([]byte(document.String())); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestJoin_PdfNumberLinesRestoresParagraphs(t *testing.T) {
	plain := "сроки работы.\n6.\n7\n. Срочные документы исполняются.\n6.1\n5\n. Снятие с контроля.\n2.1\n1.\n \nОбработка без шифрования.\n15\nКонец страницы.\n3.3. Целая строка."
	joined := join_PdfNumberLines(plain)
	want := "сроки работы.\n6.7. Срочные документы исполняются.\n6.15. Снятие с контроля.\n2.11. Обработка без шифрования.\n15\nКонец страницы.\n3.3. Целая строка."
	if joined != want {
		t.Fatalf("joined %q", joined)
	}
	paragraphs := []string{}
	for _, piece := range Split_DocumentText(joined) {
		if piece.Paragraph != "" {
			paragraphs = append(paragraphs, piece.Paragraph)
		}
	}
	if strings.Join(paragraphs, " ") != "6.7 6.15 2.11 3.3" {
		t.Fatalf("paragraphs %v", paragraphs)
	}
}

func TestExtract_DocumentChunksSplitsDataTableByRows(t *testing.T) {
	cell := func(text string, span int) string {
		properties := ""
		if span > 1 {
			properties = `<w:tcPr><w:gridSpan w:val="` + strconv.Itoa(span) + `"/></w:tcPr>`
		}
		paragraphs := ""
		for _, part := range strings.Split(text, "|") {
			paragraphs += `<w:p><w:r><w:t>` + part + `</w:t></w:r></w:p>`
		}
		return `<w:tc>` + properties + paragraphs + `</w:tc>`
	}
	row := func(cells ...string) string {
		return `<w:tr>` + strings.Join(cells, "") + `</w:tr>`
	}
	var data strings.Builder
	data.WriteString(row(cell("Вид документа", 1), cell("Юристы", 1), cell("Прочие", 2)))
	data.WriteString(row(cell("ВНЕШНИЕ ДОКУМЕНТЫ", 4)))
	for number := 1; number <= 5; number++ {
		other := "Нет"
		if number == 2 {
			other = "Департамент ИБ|Департамент ИТ"
		}
		data.WriteString(row(cell(strconv.Itoa(number)+". Письма", 1), cell("Да", 1), cell(other, 2)))
	}
	// Маленькая таблица подписи остается текстом, как в Extract_DocumentText
	signature := row(cell("Председатель", 1), cell("И. О. Фамилия", 1))
	body := build_DocxXML(t, `<w:p><w:r><w:t>Правила</w:t></w:r></w:p><w:tbl>`+data.String()+`</w:tbl><w:tbl>`+signature+`</w:tbl><w:p><w:r><w:t>Примечание</w:t></w:r></w:p>`)

	chunks, err := Extract_DocumentChunks("matrix.docx", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 7 || chunks[0].Content != "Правила" {
		t.Fatalf("chunks %+v", chunks)
	}
	second := chunks[2]
	if second.Section != "ВНЕШНИЕ ДОКУМЕНТЫ" || second.Paragraph != "2" ||
		second.Content != "ВНЕШНИЕ ДОКУМЕНТЫ\nВид документа: 2. Письма\nЮристы: Да\nПрочие: Департамент ИБ; Департамент ИТ" {
		t.Fatalf("row %+v", second)
	}
	if last := chunks[6]; last.Content != "Председатель\nИ. О. Фамилия\nПримечание" {
		t.Fatalf("tail %q", last.Content)
	}

	// Без таблицы данных фрагменты те же, что у Split_DocumentText
	plainBody := build_DocxXML(t, `<w:p><w:r><w:t>Пункт 1.1 Текст</w:t></w:r></w:p><w:tbl>`+signature+`</w:tbl>`)
	plain, err := Extract_DocumentText("note.docx", plainBody)
	if err != nil {
		t.Fatal(err)
	}
	same, err := Extract_DocumentChunks("note.docx", plainBody)
	if err != nil || !reflect.DeepEqual(same, Split_DocumentText(plain)) {
		t.Fatalf("plain docx %+v %v", same, err)
	}
}

func build_DocxXML(t *testing.T, bodyXML string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	document := `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + bodyXML + `</w:body></w:document>`
	if _, err := file.Write([]byte(document)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
