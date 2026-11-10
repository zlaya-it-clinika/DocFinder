package formatcheck

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"docfinder/core/internal/layout"
	"docfinder/core/internal/texts"
)

const bodyText = "Прошу выделить новый ноутбук для работы с клиентами, так как рабочий ноутбук вышел из строя и не подлежит ремонту."

// .docx из частей: document.xml с телом и sectPr, styles.xml, колонтитулы
func build_Docx(t *testing.T, body, section, styles string, parts map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body + section + `</w:body></w:document>`,
		"word/styles.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` + styles + `</w:styles>`,
	}
	for name, content := range parts {
		files[name] = content
	}
	for name, content := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func paragraph(props, text string) string {
	return `<w:p><w:pPr>` + props + `</w:pPr><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func footer(align string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="` + align + `"/></w:pPr><w:fldSimple w:instr=" PAGE "><w:r><w:t>2</w:t></w:r></w:fldSimple></w:p></w:ftr>`
}

func must_Format(t *testing.T) layout.Format {
	t.Helper()
	format, err := layout.Load_Format(nil)
	if err != nil {
		t.Fatal(err)
	}
	return format
}

// Стили: Times New Roman 13 пт, интервал 1,15, по ширине, отступ 1,25 см
const goodStyles = `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman"/><w:sz w:val="26"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:line="276" w:lineRule="auto"/><w:jc w:val="both"/><w:ind w:firstLine="709"/></w:pPr></w:pPrDefault></w:docDefaults>`

// A4, поля 30/20/25/15 мм, отдельная первая страница
const goodSection = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1418" w:right="1134" w:bottom="851" w:left="1701"/><w:titlePg/></w:sectPr>`

// Адресат в правой зоне, заголовок, текст, подпись
var cleanBody = paragraph(`<w:ind w:left="5000" w:firstLine="0"/><w:jc w:val="left"/>`, "Генеральному директору") +
	paragraph(`<w:ind w:left="5000" w:firstLine="0"/><w:jc w:val="left"/>`, "В. В. Капибарову") +
	paragraph(`<w:ind w:firstLine="0"/><w:jc w:val="left"/>`, "О замене ноутбука") +
	paragraph("", bodyText) + paragraph("", "Дата 05.06.2026.") +
	paragraph(`<w:ind w:firstLine="0"/><w:jc w:val="left"/>`, "Менеджер отдела продаж М. С. Петрова")

func TestCheck_DocxCleanDocument(t *testing.T) {
	findings, err := Check_Docx(build_Docx(t, cleanBody, goodSection, goodStyles, map[string]string{"word/footer1.xml": footer("right")}), must_Format(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("clean document findings: %v", findings)
	}
}

func TestCheck_DocxFindsViolations(t *testing.T) {
	// Arial 12 пт, одинарный интервал, по левому краю, поля Word по умолчанию 20/15/20/20 мм
	styles := `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:sz w:val="24"/></w:rPr></w:rPrDefault></w:docDefaults>`
	section := `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1134"/></w:sectPr>`
	body := paragraph("", bodyText) +
		paragraph("", "Приказ от «05» июня 2026 г. и письмо от 5.6.2026, подпись И.И. Иванов") +
		`<w:p><w:r><w:rPr><w:color w:val="FF0000"/></w:rPr><w:t>ДАТА / РЕГИСТРАЦИОННЫЙ НОМЕР</w:t></w:r></w:p>` +
		paragraph("", "О вводе в эксплуатацию [3] системы") +
		paragraph("", "24.10.2026 № ______")
	findings, err := Check_Docx(build_Docx(t, body, section, styles, map[string]string{"word/footer1.xml": footer("center")}), must_Format(t))
	if err != nil {
		t.Fatal(err)
	}
	joined := []string{}
	for _, finding := range findings {
		joined = append(joined, finding.String())
		if finding.Profile == "" || finding.Ref == "" {
			t.Errorf("finding without rule: %v", finding)
		}
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{
		"Левое поле 20 мм, нужно не менее 30 мм", "Правое поле 15 мм", "Верхнее поле 20 мм",
		"Шрифт Arial вместо Times New Roman", "Размер шрифта 12 пт", "Межстрочный интервал 1 ",
		"не выровнен по ширине", "Номер страницы выровнен по центру", "стоит и на первой странице",
		"«05» июня 2026", "5.6.2026", "«И.И. Иванов»", "красный текст", "номера полей",
		"Инструкция по ДОУ Организации, п. 8.16", "ГОСТ Р 7.0.97-2025, п. 3.5",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("no %q in:\n%s", want, all)
		}
	}
	// «№ ______» - место для номера при регистрации, не остаток шаблона
	if strings.Contains(all, "линии для заполнения") {
		t.Errorf("number line reported:\n%s", all)
	}
	if findings[0].Level != texts.FormatLevelError {
		t.Errorf("errors go first: %v", findings[0])
	}
	if findings[0].Clause == "" {
		t.Errorf("clause not parsed: %v", findings[0])
	}
}

func TestCheck_DocxThemeFontAndNoPageNumber(t *testing.T) {
	// Шрифт из темы (Calibri), документ длиннее страницы без номера
	styles := `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:asciiTheme="minorHAnsi"/><w:sz w:val="26"/></w:rPr></w:rPrDefault></w:docDefaults>`
	theme := `<?xml version="1.0" encoding="UTF-8"?><a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:fontScheme><a:majorFont><a:latin typeface="Calibri Light"/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`
	body := strings.Repeat(paragraph(`<w:jc w:val="both"/><w:spacing w:line="276"/><w:ind w:firstLine="709"/>`, bodyText), 30)
	findings, err := Check_Docx(build_Docx(t, body, goodSection, styles, map[string]string{"word/theme/theme1.xml": theme}), must_Format(t))
	if err != nil {
		t.Fatal(err)
	}
	all := ""
	for _, finding := range findings {
		all += finding.String() + "\n"
	}
	if !strings.Contains(all, "Шрифт Calibri") || !strings.Contains(all, "Нет номера страницы") {
		t.Fatalf("findings:\n%s", all)
	}
}

func TestCheck_DocxRejectsNotDocx(t *testing.T) {
	if _, err := Check_Docx([]byte("plain text"), must_Format(t)); err != ErrNotDocx {
		t.Fatalf("err %v", err)
	}
}

func read_Entry(t *testing.T, body []byte, name string) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if entry.Name == name {
			reader, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			var buffer bytes.Buffer
			if _, err := buffer.ReadFrom(reader); err != nil {
				t.Fatal(err)
			}
			return buffer.Bytes()
		}
	}
	t.Fatalf("no %s", name)
	return nil
}
