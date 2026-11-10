package formatcheck

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"docfinder/core/internal/layout"
	"docfinder/core/internal/texts"
)

// Исправление оформления .docx по тем же правилам, что и проверка. Меняется только оформление:
// лист, поля, номер страницы, шрифт, размер, интервал, выравнивание и отступ основного текста,
// формат дат и пробел в инициалах. Слова документа, остатки шаблона и расположение колонтитулов не трогаются -
// они остаются в замечаниях после исправления

var (
	textElement  = regexp.MustCompile(`(<w:t(?:\s[^>]*)?>)([^<]*)(</w:t>)`)
	fontsElement = regexp.MustCompile(`<w:rFonts\b[^>]*?/>`)
	quotedDate   = regexp.MustCompile(`«\s*(\d{1,2})\s*»\s*([а-яё]+)\s+(\d{4})(\s*г\.)?`)
	stylesOpen   = regexp.MustCompile(`<w:styles\b[^>]*>`)
	monthNumbers = map[string]int{
		"января": 1, "февраля": 2, "марта": 3, "апреля": 4, "мая": 5, "июня": 6,
		"июля": 7, "августа": 8, "сентября": 9, "октября": 10, "ноября": 11, "декабря": 12,
	}
)

// Что исправлено: тексты для ответа сотруднику
type FixResult struct {
	Body    []byte
	Applied []string
}

func Fix_Docx(body []byte, format layout.Format, options FixOptions) (FixResult, error) {
	info, err := read_Document(body)
	if err != nil {
		return FixResult{}, err
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return FixResult{}, ErrNotDocx
	}
	files := map[string][]byte{}
	for _, entry := range archive.File {
		if !strings.HasSuffix(entry.Name, ".xml") && !strings.HasSuffix(entry.Name, ".rels") {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return FixResult{}, ErrNotDocx
		}
		raw, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return FixResult{}, ErrNotDocx
		}
		files[entry.Name] = raw
	}
	fixer := &fixer{format: format, info: info, options: options, sheet: read_StyleSheet(files["word/styles.xml"], files["word/theme/theme1.xml"]), applied: map[string]bool{}}
	fixer.plan_PageNumber(files)
	document, err := fixer.fix_Document(files["word/document.xml"])
	if err != nil {
		return FixResult{}, err
	}
	files["word/document.xml"] = document
	added := fixer.add_PageNumberPart(files)
	if styles, found := files["word/styles.xml"]; found {
		files["word/styles.xml"] = fixer.fix_Styles(styles)
	}
	for name, raw := range files {
		part := ""
		switch {
		case strings.HasPrefix(name, "word/header"):
			part = "header"
		case strings.HasPrefix(name, "word/footer"):
			part = "footer"
		default:
			continue
		}
		files[name] = fixer.fix_Fonts(fixer.fix_PageNumber(raw, part))
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range archive.File {
		header := entry.FileHeader
		target, err := writer.CreateHeader(&header)
		if err != nil {
			return FixResult{}, err
		}
		if raw, changed := files[entry.Name]; changed {
			if _, err := target.Write(raw); err != nil {
				return FixResult{}, err
			}
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return FixResult{}, err
		}
		_, err = io.Copy(target, reader)
		reader.Close()
		if err != nil {
			return FixResult{}, err
		}
	}
	for name, raw := range added {
		target, err := writer.Create(name)
		if err != nil {
			return FixResult{}, err
		}
		if _, err := target.Write(raw); err != nil {
			return FixResult{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return FixResult{}, err
	}
	return FixResult{Body: output.Bytes(), Applied: fixer.applied_Texts()}, nil
}

type fixer struct {
	format  layout.Format
	info    documentInfo
	options FixOptions
	sheet   styleSheet
	applied map[string]bool
	// Номер страницы добавляется отдельным колонтитулом: его часть и связь в document.xml.rels
	pageNumberPart  string
	pageNumberAdded bool
}

func (fixer *fixer) applied_Texts() []string {
	order := []struct {
		key  string
		text string
	}{
		{"page", texts.FormatFixPage}, {"margins", texts.FormatFixMargins}, {"firstPage", texts.FormatFixFirstPage},
		{"font", texts.FormatFixFont}, {"size", texts.FormatFixSize}, {"spacing", texts.FormatFixSpacing},
		{"align", texts.FormatFixAlign}, {"indent", texts.FormatFixIndent}, {"pageNumber", texts.FormatFixPageNumber},
		{"dates", texts.FormatFixDates}, {"initials", texts.FormatFixInitials},
		{"addressee", texts.FormatFixAddressee}, {"title", texts.FormatFixTitle}, {"signature", texts.FormatFixSignature},
		{"pageNumberAdded", texts.FormatFixPageNumberAdded},
	}
	applied := []string{}
	for _, item := range order {
		if fixer.applied[item.key] {
			applied = append(applied, item.text)
		}
	}
	return applied
}

func twips(mm float64) string {
	return strconv.Itoa(int(math.Round(mm * twipsPerMM)))
}

func (fixer *fixer) fix_Document(raw []byte) ([]byte, error) {
	spans, err := scan_Elements(raw)
	if err != nil {
		return nil, err
	}
	edits := []xmlEdit{}
	edited := []xmlSpan{}
	// Вставки реквизитов: позиция в исходном тексте и что вставить перед ней
	firstBody := -1
	for _, span := range spans {
		if span.Name != "p" || !span.has_Ancestor("body") || span.has_Ancestor("tbl") || span.has_Ancestor("p") || span.has_Ancestor("txbxContent") {
			continue
		}
		fragment := string(raw[span.Start:span.End])
		paragraph, ok := read_ParagraphFragment(fragment)
		if !ok {
			continue
		}
		resolved := fixer.sheet.resolve_Paragraph(paragraph, false)
		if !check_TextParagraph(resolved) {
			continue
		}
		if firstBody < 0 && check_BodyParagraph(resolved) {
			firstBody = span.Start
		}
		fixed, changed, err := fixer.fix_BodyParagraph(fragment, resolved, check_BodyParagraph(resolved))
		if err != nil || !changed {
			continue
		}
		edits = append(edits, xmlEdit{Start: span.Start, End: span.End, Text: fixed})
		edited = append(edited, span)
	}
	for _, span := range spans {
		if span.Name != "sectPr" {
			continue
		}
		inside := false
		for _, paragraph := range edited {
			if span.Start >= paragraph.Start && span.End <= paragraph.End {
				inside = true
			}
		}
		if inside {
			continue
		}
		bodyLevel := len(span.Path) > 0 && span.Path[len(span.Path)-1] == "body"
		if fixed, changed := fixer.fix_Section(string(raw[span.Start:span.End]), bodyLevel); changed {
			edits = append(edits, xmlEdit{Start: span.Start, End: span.End, Text: fixed})
		}
	}
	edits = fixer.insert_Requisites(raw, spans, firstBody, edits)
	result := apply_Edits(raw, edits)
	result = fixer.fix_Fonts(result)
	return fixer.fix_Text(result), nil
}

// Абзац из фрагмента документа: префикс w: объявляется в обертке
func read_ParagraphFragment(fragment string) (xParagraph, bool) {
	var wrapper struct {
		Paragraph xParagraph `xml:"p"`
	}
	raw := `<root xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` + fragment + `</root>`
	if xml.Unmarshal([]byte(raw), &wrapper) != nil {
		return xParagraph{}, false
	}
	return wrapper.Paragraph, true
}

func (fixer *fixer) fix_Section(raw string, bodyLevel bool) (string, bool) {
	format := fixer.format
	props, err := parse_Props(raw, sectionPropsOrder)
	if err != nil {
		return raw, false
	}
	changed := false
	if size := props.get("pgSz"); format.PageWidthMM > 0 {
		width := twips_ToMM(read_Attribute(size, "w:w"))
		height := twips_ToMM(read_Attribute(size, "w:h"))
		portrait := near(width, format.PageWidthMM, pageToleranceMM) && near(height, format.PageHeightMM, pageToleranceMM)
		landscape := near(width, format.PageHeightMM, pageToleranceMM) && near(height, format.PageWidthMM, pageToleranceMM)
		if size == "" || (!portrait && !landscape) {
			newWidth, newHeight := format.PageWidthMM, format.PageHeightMM
			if width > height {
				newWidth, newHeight = newHeight, newWidth
			}
			if size == "" {
				size = `<w:pgSz/>`
			}
			props.set("pgSz", set_Attributes(size, map[string]string{"w:w": twips(newWidth), "w:h": twips(newHeight)}))
			fixer.applied["page"] = true
			changed = true
		}
	}
	margin := props.get("pgMar")
	if margin == "" {
		margin = `<w:pgMar w:header="709" w:footer="709" w:gutter="0"/>`
	}
	values := map[string]string{}
	for _, item := range []struct {
		key  string
		need float64
	}{
		{"w:left", format.MarginLeftMM}, {"w:right", format.MarginRightMM}, {"w:top", format.MarginTopMM}, {"w:bottom", format.MarginBottomMM},
	} {
		actual := read_Attribute(margin, item.key)
		if item.need > 0 && (actual == "" || twips_ToMM(actual) < item.need-marginToleranceMM) {
			values[item.key] = twips(item.need)
		}
	}
	if len(values) > 0 {
		props.set("pgMar", set_Attributes(margin, values))
		fixer.applied["margins"] = true
		changed = true
	}
	// Номера страниц нет совсем: колонтитул с номером по правилам на все страницы раздела
	if bodyLevel && fixer.pageNumberPart != "" {
		kind := "footerReference"
		if format.PageNumberPart == "header" {
			kind = "headerReference"
		}
		if !strings.Contains(raw, "<w:"+kind+` w:type="default"`) {
			props.Children = append([]xmlChild{{Name: kind, Raw: `<w:` + kind + ` w:type="default" r:id="` + pageNumberRelation + `"/>`}}, props.Children...)
			fixer.pageNumberAdded = true
			fixer.applied["pageNumberAdded"] = true
			changed = true
		}
	}
	// Номер не на первой странице: отдельный колонтитул первой страницы, у которого номера нет
	if !format.PageNumberOnFirstPage && (len(fixer.info.PageNumbers) > 0 || fixer.pageNumberAdded) {
		if titlePage := props.get("titlePg"); titlePage == "" || !check_OnOff(&xOnOff{Val: optional_Attribute(titlePage, "w:val")}) {
			props.set("titlePg", `<w:titlePg/>`)
			fixer.applied["firstPage"] = true
			changed = true
		}
	}
	return props.String(), changed
}

func optional_Attribute(raw, key string) *string {
	if !strings.Contains(openTag.FindString(raw), key+`="`) {
		return nil
	}
	value := read_Attribute(raw, key)
	return &value
}

// Абзац с текстом: размер шрифта. Абзац основного текста (body) - еще выравнивание, интервал и отступ
func (fixer *fixer) fix_BodyParagraph(fragment string, resolved paragraphInfo, body bool) (string, bool, error) {
	format := fixer.format
	spans, err := scan_Elements([]byte(fragment))
	if err != nil {
		return fragment, false, err
	}
	root := spans[0]
	edits := []xmlEdit{}
	changed := false
	var paragraphProps *xmlSpan
	for index, child := range direct_Children(spans, root) {
		if child.Name == "pPr" {
			paragraphProps = &direct_Children(spans, root)[index]
		}
	}
	propsRaw := `<w:pPr></w:pPr>`
	if paragraphProps != nil {
		propsRaw = fragment[paragraphProps.Start:paragraphProps.End]
	}
	props, err := parse_Props(propsRaw, paragraphPropsOrder)
	if err != nil {
		return fragment, false, err
	}
	propsChanged := false
	if !body {
		format.TextAlign, format.TextLineSpacing, format.FirstLineIndentMM = "", 0, 0
	}
	if format.TextAlign == "both" && !justifiedAligns[resolved.Align] {
		props.set("jc", `<w:jc w:val="both"/>`)
		fixer.applied["align"] = true
		propsChanged = true
	}
	if format.TextLineSpacing > 0 && (resolved.LineSpacing == 0 || resolved.LineSpacing < format.TextLineSpacing-spacingTolerance || resolved.LineSpacing > maxTextLineSpacing+spacingTolerance) {
		spacing := props.get("spacing")
		if spacing == "" {
			spacing = `<w:spacing/>`
		}
		props.set("spacing", set_Attributes(spacing, map[string]string{"w:line": strconv.Itoa(int(math.Round(format.TextLineSpacing * 240))), "w:lineRule": "auto"}))
		fixer.applied["spacing"] = true
		propsChanged = true
	}
	if format.FirstLineIndentMM > 0 && math.Abs(resolved.FirstLineMM-format.FirstLineIndentMM) > indentToleranceMM {
		indent := props.get("ind")
		if indent == "" {
			indent = `<w:ind/>`
		}
		props.set("ind", set_Attributes(indent, map[string]string{"w:firstLine": twips(format.FirstLineIndentMM), "w:hanging": "", "w:firstLineChars": "", "w:hangingChars": ""}))
		fixer.applied["indent"] = true
		propsChanged = true
	}
	if propsChanged {
		changed = true
		if paragraphProps != nil {
			edits = append(edits, xmlEdit{Start: paragraphProps.Start, End: paragraphProps.End, Text: props.String()})
		} else {
			openEnd := len(openTag.FindString(fragment))
			edits = append(edits, xmlEdit{Start: openEnd, End: openEnd, Text: props.String()})
		}
	}
	if format.FontSizePt > 0 {
		size := strconv.Itoa(int(math.Round(format.FontSizePt * 2)))
		wrongSize := false
		for _, run := range resolved.Runs {
			if strings.TrimSpace(run.Text) != "" && run.SizePt != 0 && !near(run.SizePt, format.FontSizePt, 0.01) {
				wrongSize = true
			}
		}
		if wrongSize {
			for _, run := range spans {
				if run.Name != "r" || run.has_Ancestor("pPr") || run.Start <= root.Start {
					continue
				}
				var runProps *xmlSpan
				children := direct_Children(spans, run)
				for index, child := range children {
					if child.Name == "rPr" {
						runProps = &children[index]
					}
				}
				raw := `<w:rPr></w:rPr>`
				if runProps != nil {
					raw = fragment[runProps.Start:runProps.End]
				}
				props, err := parse_Props(raw, runPropsOrder)
				if err != nil {
					continue
				}
				props.set("sz", `<w:sz w:val="`+size+`"/>`)
				props.set("szCs", `<w:szCs w:val="`+size+`"/>`)
				if runProps != nil {
					edits = append(edits, xmlEdit{Start: runProps.Start, End: runProps.End, Text: props.String()})
				} else {
					openEnd := run.Start + len(openTag.FindString(fragment[run.Start:]))
					edits = append(edits, xmlEdit{Start: openEnd, End: openEnd, Text: props.String()})
				}
			}
			fixer.applied["size"] = true
			changed = true
		}
	}
	return string(apply_Edits([]byte(fragment), edits)), changed, nil
}

// Шрифт: все rFonts документа, стилей и колонтитулов - на шрифт правил, шрифты темы заменяются явным
func (fixer *fixer) fix_Fonts(raw []byte) []byte {
	font := fixer.format.FontName
	if font == "" || len(fixer.info.Paragraphs) == 0 {
		return raw
	}
	wanted := `<w:rFonts w:ascii="` + font + `" w:hAnsi="` + font + `" w:cs="` + font + `" w:eastAsia="` + font + `"/>`
	return fontsElement.ReplaceAllFunc(raw, func(match []byte) []byte {
		if string(match) != wanted {
			fixer.applied["font"] = fixer.applied["font"] || fixer.has_WrongFont()
		}
		return []byte(wanted)
	})
}

func (fixer *fixer) has_WrongFont() bool {
	for _, paragraph := range fixer.info.Paragraphs {
		for _, run := range paragraph.Runs {
			if strings.TrimSpace(run.Text) != "" && run.Font != "" && !strings.EqualFold(run.Font, fixer.format.FontName) {
				return true
			}
		}
	}
	return false
}

// Стили: шрифт по умолчанию задается явно, иначе текст без rFonts берет шрифт темы
func (fixer *fixer) fix_Styles(raw []byte) []byte {
	raw = fixer.fix_Fonts(raw)
	font := fixer.format.FontName
	if font == "" {
		return raw
	}
	fonts := `<w:rFonts w:ascii="` + font + `" w:hAnsi="` + font + `" w:cs="` + font + `" w:eastAsia="` + font + `"/>`
	spans, err := scan_Elements(raw)
	if err != nil {
		return raw
	}
	for _, span := range spans {
		if span.Name == "rPr" && span.has_Ancestor("rPrDefault") {
			props, err := parse_Props(string(raw[span.Start:span.End]), runPropsOrder)
			if err != nil || props.get("rFonts") != "" {
				return raw
			}
			props.set("rFonts", fonts)
			if fixer.has_WrongFont() {
				fixer.applied["font"] = true
			}
			return apply_Edits(raw, []xmlEdit{{Start: span.Start, End: span.End, Text: props.String()}})
		}
	}
	for _, span := range spans {
		if span.Name == "rPrDefault" {
			return raw
		}
	}
	defaults := `<w:rPrDefault><w:rPr>` + fonts + `</w:rPr></w:rPrDefault>`
	for _, span := range spans {
		if span.Name == "docDefaults" {
			openEnd := span.Start + len(openTag.FindString(string(raw[span.Start:])))
			return apply_Edits(raw, []xmlEdit{{Start: openEnd, End: openEnd, Text: defaults}})
		}
	}
	if open := stylesOpen.FindIndex(raw); open != nil {
		if fixer.has_WrongFont() {
			fixer.applied["font"] = true
		}
		return apply_Edits(raw, []xmlEdit{{Start: open[1], End: open[1], Text: `<w:docDefaults>` + defaults + `</w:docDefaults>`}})
	}
	return raw
}

// Номер страницы в нужном колонтитуле выравнивается по правилу. Перенос между верхним и нижним остается вручную
func (fixer *fixer) fix_PageNumber(raw []byte, part string) []byte {
	format := fixer.format
	if format.PageNumberAlign == "" || part != format.PageNumberPart {
		return raw
	}
	spans, err := scan_Elements(raw)
	if err != nil {
		return raw
	}
	edits := []xmlEdit{}
	for _, span := range spans {
		if span.Name != "p" || span.has_Ancestor("p") {
			continue
		}
		fragment := string(raw[span.Start:span.End])
		paragraph, ok := read_ParagraphFragment(fragment)
		if !ok || !check_HasPageField(paragraph) {
			continue
		}
		if fixer.sheet.resolve_Paragraph(paragraph, false).Align == format.PageNumberAlign {
			continue
		}
		inner, err := scan_Elements([]byte(fragment))
		if err != nil {
			continue
		}
		var propsSpan *xmlSpan
		children := direct_Children(inner, inner[0])
		for index, child := range children {
			if child.Name == "pPr" {
				propsSpan = &children[index]
			}
		}
		propsRaw := `<w:pPr></w:pPr>`
		if propsSpan != nil {
			propsRaw = fragment[propsSpan.Start:propsSpan.End]
		}
		props, err := parse_Props(propsRaw, paragraphPropsOrder)
		if err != nil {
			continue
		}
		props.set("jc", `<w:jc w:val="`+format.PageNumberAlign+`"/>`)
		var fixed string
		if propsSpan != nil {
			fixed = string(apply_Edits([]byte(fragment), []xmlEdit{{Start: propsSpan.Start, End: propsSpan.End, Text: props.String()}}))
		} else {
			openEnd := len(openTag.FindString(fragment))
			fixed = fragment[:openEnd] + props.String() + fragment[openEnd:]
		}
		edits = append(edits, xmlEdit{Start: span.Start, End: span.End, Text: fixed})
		fixer.applied["pageNumber"] = true
	}
	return apply_Edits(raw, edits)
}

// Даты и инициалы в тексте. Правится текст внутри одного фрагмента w:t: дата, разорванная форматированием, остается в замечаниях
func (fixer *fixer) fix_Text(raw []byte) []byte {
	format := fixer.format
	return textElement.ReplaceAllFunc(raw, func(match []byte) []byte {
		parts := textElement.FindSubmatch(match)
		text := string(parts[2])
		original := text
		if format.DateFormat != "" {
			text = quotedDate.ReplaceAllStringFunc(text, func(date string) string {
				pieces := quotedDate.FindStringSubmatch(date)
				month, known := monthNumbers[strings.ToLower(pieces[2])]
				day, _ := strconv.Atoi(pieces[1])
				if !known {
					return date
				}
				fixer.applied["dates"] = true
				return fmt_Date(format.DateFormat, day, month, pieces[3])
			})
			text = dateShort.ReplaceAllStringFunc(text, func(found string) string {
				pieces := dateShort.FindStringSubmatch(found)
				if !check_ShortDate(pieces[2]) {
					return found
				}
				numbers := strings.Split(pieces[2], ".")
				day, _ := strconv.Atoi(numbers[0])
				month, _ := strconv.Atoi(numbers[1])
				fixer.applied["dates"] = true
				return pieces[1] + fmt_Date(format.DateFormat, day, month, numbers[2]) + pieces[3]
			})
		}
		switch format.InitialsSeparator {
		case " ":
			text = initialsTight.ReplaceAllStringFunc(text, func(found string) string {
				fixer.applied["initials"] = true
				return regexp.MustCompile(`([А-ЯЁ]\.)([А-ЯЁ]\.)`).ReplaceAllString(found, "$1 $2")
			})
		case "":
			text = initialsSpaced.ReplaceAllStringFunc(text, func(found string) string {
				fixer.applied["initials"] = true
				return regexp.MustCompile(`([А-ЯЁ]\.)\s+([А-ЯЁ]\.)`).ReplaceAllString(found, "$1$2")
			})
		}
		if text == original {
			return match
		}
		return []byte(string(parts[1]) + text + string(parts[3]))
	})
}

func fmt_Date(layoutText string, day, month int, year string) string {
	return strings.NewReplacer("DD", two_Digits(day), "MM", two_Digits(month), "YYYY", year).Replace(layoutText)
}

func two_Digits(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

const (
	pageNumberRelation = "rIdDocFinderPage"
	pageNumberRelType  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	pageNumberMimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml."
)

// Номера страницы нет, а текст длиннее страницы: колонтитул с номером будет добавлен.
// Нужен префикс r: в document.xml - через него sectPr ссылается на колонтитул
func (fixer *fixer) plan_PageNumber(files map[string][]byte) {
	format := fixer.format
	if len(fixer.info.PageNumbers) > 0 || format.PageNumberPart == "" || !bytes.Contains(files["word/document.xml"], []byte(`xmlns:r=`)) ||
		files["word/_rels/document.xml.rels"] == nil || files["[Content_Types].xml"] == nil {
		return
	}
	characters := 0
	for _, paragraph := range fixer.info.Paragraphs {
		characters += utf8.RuneCountInString(paragraph.Text)
	}
	if characters <= texts.FormatPageRunes {
		return
	}
	fixer.pageNumberPart = "word/" + format.PageNumberPart + "DocFinder.xml"
}

// Часть колонтитула с полем PAGE, связь и тип содержимого. Возвращает новые файлы архива
func (fixer *fixer) add_PageNumberPart(files map[string][]byte) map[string][]byte {
	if !fixer.pageNumberAdded {
		return nil
	}
	format := fixer.format
	root, kind := "w:ftr", "footer"
	if format.PageNumberPart == "header" {
		root, kind = "w:hdr", "header"
	}
	align := format.PageNumberAlign
	if align == "" {
		align = "right"
	}
	runProps := fixer.run_Props()
	part := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<` + root + ` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<w:p><w:pPr><w:jc w:val="` + align + `"/></w:pPr>` +
		`<w:r>` + runProps + `<w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r>` + runProps + `<w:instrText xml:space="preserve"> PAGE </w:instrText></w:r>` +
		`<w:r>` + runProps + `<w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r>` + runProps + `<w:t>2</w:t></w:r>` +
		`<w:r>` + runProps + `<w:fldChar w:fldCharType="end"/></w:r></w:p></` + root + `>`
	target := strings.TrimPrefix(fixer.pageNumberPart, "word/")
	relation := `<Relationship Id="` + pageNumberRelation + `" Type="` + pageNumberRelType + kind + `" Target="` + target + `"/>`
	files["word/_rels/document.xml.rels"] = bytes.Replace(files["word/_rels/document.xml.rels"], []byte("</Relationships>"), []byte(relation+"</Relationships>"), 1)
	override := `<Override PartName="/` + fixer.pageNumberPart + `" ContentType="` + pageNumberMimeType + kind + `+xml"/>`
	files["[Content_Types].xml"] = bytes.Replace(files["[Content_Types].xml"], []byte("</Types>"), []byte(override+"</Types>"), 1)
	return map[string][]byte{fixer.pageNumberPart: []byte(part)}
}

// Адресат в начало, заголовок перед первым абзацем основного текста, подпись в конец перед параметрами страницы.
// Вставка в позицию, с которой начинается другая правка, дописывается в начало этой правки
func (fixer *fixer) insert_Requisites(raw []byte, spans []xmlSpan, firstBody int, edits []xmlEdit) []xmlEdit {
	options := fixer.options
	inserts := []xmlEdit{}
	bodyStart, bodyEnd, sectionStart := -1, -1, -1
	for _, span := range spans {
		if span.Name == "body" {
			bodyStart = span.Start + len(openTag.FindString(string(raw[span.Start:])))
			bodyEnd = span.End - len("</w:body>")
		}
		if span.Name == "sectPr" && len(span.Path) > 0 && span.Path[len(span.Path)-1] == "body" {
			sectionStart = span.Start
		}
	}
	if bodyStart < 0 {
		return edits
	}
	if len(options.Addressee) > 0 {
		inserts = append(inserts, xmlEdit{Start: bodyStart, End: bodyStart, Text: fixer.addressee_XML(options.Addressee)})
		fixer.applied["addressee"] = true
	}
	if title := strings.TrimSpace(options.Title); title != "" {
		at := firstBody
		if at < 0 {
			at = bodyStart
		}
		inserts = append(inserts, xmlEdit{Start: at, End: at, Text: fixer.title_XML(title)})
		fixer.applied["title"] = true
	}
	if strings.TrimSpace(options.SignerName) != "" {
		at := sectionStart
		if at < 0 {
			at = bodyEnd
		}
		inserts = append(inserts, xmlEdit{Start: at, End: at, Text: fixer.signature_XML(options.SignerPosition, options.SignerName)})
		fixer.applied["signature"] = true
	}
	// Вставки в одну позицию склеиваются по порядку: адресат, затем заголовок
	grouped := []xmlEdit{}
	for _, insert := range inserts {
		if last := len(grouped) - 1; last >= 0 && grouped[last].Start == insert.Start {
			grouped[last].Text += insert.Text
			continue
		}
		grouped = append(grouped, insert)
	}
	for _, insert := range grouped {
		merged := false
		for index := range edits {
			if edits[index].Start == insert.Start {
				edits[index].Text = insert.Text + edits[index].Text
				merged = true
				break
			}
		}
		if !merged {
			edits = append(edits, insert)
		}
	}
	return edits
}
