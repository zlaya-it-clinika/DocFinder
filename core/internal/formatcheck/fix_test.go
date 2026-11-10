package formatcheck

import (
	"strings"
	"testing"

	"docfinder/core/internal/texts"
)

func TestFix_DocxFixesFormatting(t *testing.T) {
	format := must_Format(t)
	styles := `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:asciiTheme="minorHAnsi" w:hAnsiTheme="minorHAnsi"/><w:sz w:val="24"/><w:lang w:val="ru-RU"/></w:rPr></w:rPrDefault></w:docDefaults>`
	section := `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1134" w:header="708" w:footer="708" w:gutter="0"/><w:cols w:space="708"/></w:sectPr>`
	body := `<w:p><w:pPr><w:spacing w:after="160" w:line="240" w:lineRule="auto"/><w:ind w:left="0"/></w:pPr>` +
		`<w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:b/><w:lang w:val="ru-RU"/></w:rPr><w:t xml:space="preserve">` + bodyText + `</w:t></w:r></w:p>` +
		paragraph("", "Приказ от «05» июня 2026 г. и письмо от 5.6.2026, подпись И.И. Иванов") +
		`<w:p><w:r><w:rPr><w:color w:val="FF0000"/></w:rPr><w:t>ДАТА / РЕГИСТРАЦИОННЫЙ НОМЕР</w:t></w:r></w:p>`
	theme := `<?xml version="1.0" encoding="UTF-8"?><a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:fontScheme><a:majorFont><a:latin typeface="Calibri Light"/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`
	original := build_Docx(t, body, section, styles, map[string]string{"word/footer1.xml": footer("center"), "word/theme/theme1.xml": theme})
	before, err := Check_Docx(original, format)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Fix_Docx(original, format, FixOptions{
		Addressee:      []string{"Генеральному директору", "В. В. Капибарову"},
		Title:          "О замене ноутбука",
		SignerPosition: "Менеджер отдела продаж",
		SignerName:     "М. С. Петрова",
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := Check_Docx(result.Body, format)
	if err != nil {
		t.Fatalf("fixed docx does not parse: %v", err)
	}
	remaining := ""
	for _, finding := range after {
		remaining += finding.String() + "\n"
	}
	// Красный текст - содержание, его убирает человек. Остальное исправлено
	if len(after) != 1 || !strings.Contains(remaining, "красный текст") {
		t.Fatalf("before %d, remaining:\n%s", len(before), remaining)
	}
	for _, want := range []string{texts.FormatFixPage, texts.FormatFixMargins, texts.FormatFixFirstPage, texts.FormatFixFont, texts.FormatFixSize,
		texts.FormatFixSpacing, texts.FormatFixAlign, texts.FormatFixIndent, texts.FormatFixPageNumber, texts.FormatFixDates, texts.FormatFixInitials,
		texts.FormatFixAddressee, texts.FormatFixTitle, texts.FormatFixSignature} {
		found := false
		for _, applied := range result.Applied {
			found = found || applied == want
		}
		if !found {
			t.Errorf("not applied %q: %v", want, result.Applied)
		}
	}
	info, _ := read_Document(result.Body)
	all := ""
	for _, paragraph := range info.Paragraphs {
		all += paragraph.Text + "\n"
	}
	if !strings.Contains(all, "от 05.06.2026 и письмо от 05.06.2026, подпись И. И. Иванов") || !strings.Contains(all, bodyText) ||
		!strings.HasPrefix(all, "Генеральному директору\nВ. В. Капибарову\nО замене ноутбука\n"+bodyText) || !strings.HasSuffix(all, "Менеджер отдела продаж М. С. Петрова\n") {
		t.Fatalf("text after fix:\n%s", all)
	}
	// Порядок свойств по схеме: полужирный и язык сохранены, размер встал между ними
	document := string(read_Entry(t, result.Body, "word/document.xml"))
	if !strings.Contains(document, `<w:b/><w:sz w:val="26"/><w:szCs w:val="26"/><w:lang w:val="ru-RU"/>`) {
		t.Fatalf("run props order: %s", document)
	}
	if !strings.Contains(document, `w:after="160"`) || !strings.Contains(document, `<w:cols w:space="708"/><w:titlePg/>`) {
		t.Fatalf("props lost: %s", document)
	}
}

func TestFix_DocxKeepsCleanDocument(t *testing.T) {
	original := build_Docx(t, cleanBody, goodSection, goodStyles, map[string]string{"word/footer1.xml": footer("right")})
	result, err := Fix_Docx(original, must_Format(t), FixOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("clean document changed: %v", result.Applied)
	}
}

func TestFix_DocxShortParagraphsAndPageNumber(t *testing.T) {
	format := must_Format(t)
	// Как в рецепте: стиль Calibri 12, короткие шаги и длинные абзацы, номера страницы нет, текст длиннее страницы
	styles := `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri"/><w:sz w:val="24"/></w:rPr></w:rPrDefault></w:docDefaults>`
	section := `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1418" w:right="1134" w:bottom="851" w:left="1701"/></w:sectPr>`
	body := strings.Repeat(paragraph("", bodyText)+paragraph("", "Добавляем томатное пюре, перемешиваем.")+
		paragraph(`<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr><w:ind w:left="720" w:hanging="360"/>`, "Пункт списка остается списком.")+
		paragraph(`<w:jc w:val="center"/>`, "Заголовок по центру"), 25)
	result, err := Fix_Docx(build_DocxWithRelations(t, body, section, styles), format, FixOptions{})
	if err != nil {
		t.Fatal(err)
	}
	info, _ := read_Document(result.Body)
	for _, paragraph := range info.Paragraphs {
		switch {
		case strings.HasPrefix(paragraph.Text, "Добавляем"):
			if paragraph.Align != "both" || paragraph.FirstLineMM < 12 || paragraph.Runs[0].SizePt != format.FontSizePt {
				t.Fatalf("short body paragraph not fixed: %+v", paragraph)
			}
		case strings.HasPrefix(paragraph.Text, "Пункт"):
			if paragraph.Align == "both" || paragraph.FirstLineMM >= 0 {
				t.Fatalf("list item changed: %+v", paragraph)
			}
		case strings.HasPrefix(paragraph.Text, "Заголовок"):
			if paragraph.Align != "center" {
				t.Fatalf("centered line changed: %+v", paragraph)
			}
		}
	}
	if len(info.PageNumbers) != 1 || info.PageNumbers[0].Part != format.PageNumberPart || info.PageNumbers[0].Align != format.PageNumberAlign || !info.TitlePage {
		t.Fatalf("page number %+v title %v", info.PageNumbers, info.TitlePage)
	}
	rels := string(read_Entry(t, result.Body, "word/_rels/document.xml.rels"))
	types := string(read_Entry(t, result.Body, "[Content_Types].xml"))
	if !strings.Contains(rels, pageNumberRelation) || !strings.Contains(types, "/word/"+format.PageNumberPart+"DocFinder.xml") {
		t.Fatalf("relations %s types %s", rels, types)
	}
}

func build_DocxWithRelations(t *testing.T, body, section, styles string) []byte {
	t.Helper()
	return build_Docx(t, "", "", styles, map[string]string{
		"word/document.xml":            `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` + body + section + `</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"[Content_Types].xml":          `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
	})
}
