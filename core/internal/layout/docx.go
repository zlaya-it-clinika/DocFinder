package layout

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"

	"docfinder/core/internal/texts"
)

// Сборка .docx. Размеры, шрифт, интервалы и место номера страницы - из правил оформления (format.go):
// Инструкция по ДОУ важнее, о чем она молчит - по ГОСТ Р 7.0.97-2025

// Единицы Word: twip - 1/20 пункта, EMU - единица картинок, интервал «auto» - в 1/240 строки
const (
	twipsPerMM       = 1440.0 / 25.4
	emuPerMM         = 36000
	lineSpacingUnits = 240
)

// Части пакета с номером страницы: колонтитул с номером и пустой колонтитул первой страницы
const (
	pageNumberRelation = "rIdPageNumber"
	pageFirstRelation  = "rIdPageFirst"
)

type signature struct {
	Position string
	Name     string
}

type document struct {
	Kind   string
	Emblem *Image
	Stamp  *Image
	// Реквизиты в правой зоне: адресат, ниже составитель записки
	HeaderRight []string
	TypeTitle   string
	DateLine    string
	Place       string
	Title       string
	Preamble    []string
	Greeting    string
	Text        []string
	Attach      string
	Closing     string
	Signatures  []signature
	Executor    string
}

func twips(mm float64) int {
	return int(mm*twipsPerMM + 0.5)
}

type paragraphStyle struct {
	align    string
	indentMM float64
	// Абзацный отступ и межстрочный интервал текста. Без него - интервал реквизитов по умолчанию
	bodyText bool
	bold     bool
	small    bool
	spaceMM  float64
	tabRight bool
	// Абзац в рамке у нижнего края текста страницы: отметка об исполнителе
	pageBottom bool
}

type docxWriter struct {
	format Format
	body   strings.Builder
	images []docxImage
}

type docxImage struct {
	id          string
	name        string
	contentType string
	body        []byte
}

func (writer *docxWriter) paragraph(text string, style paragraphStyle) {
	writer.body.WriteString(writer.paragraph_XML(text, style))
}

func (writer *docxWriter) paragraph_XML(text string, style paragraphStyle) string {
	format := writer.format
	var builder strings.Builder
	builder.WriteString(`<w:p><w:pPr>`)
	if style.pageBottom {
		fmt.Fprintf(&builder, `<w:framePr w:w="%d" w:wrap="around" w:vAnchor="margin" w:hAnchor="margin" w:xAlign="left" w:yAlign="bottom"/>`, twips(format.text_WidthMM()))
	}
	if style.tabRight {
		fmt.Fprintf(&builder, `<w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs>`, twips(format.text_WidthMM()))
	}
	if style.spaceMM > 0 || style.bodyText {
		builder.WriteString(`<w:spacing`)
		if style.spaceMM > 0 {
			fmt.Fprintf(&builder, ` w:before="%d"`, twips(style.spaceMM))
		}
		if style.bodyText {
			fmt.Fprintf(&builder, ` w:line="%d" w:lineRule="auto"`, line_Spacing(format.TextLineSpacing))
		}
		builder.WriteString(`/>`)
	}
	if style.indentMM > 0 || style.bodyText {
		builder.WriteString(`<w:ind`)
		if style.indentMM > 0 {
			fmt.Fprintf(&builder, ` w:left="%d"`, twips(style.indentMM))
		}
		if style.bodyText {
			fmt.Fprintf(&builder, ` w:firstLine="%d"`, twips(format.FirstLineIndentMM))
		}
		builder.WriteString(`/>`)
	}
	align := style.align
	if style.bodyText && align == "" {
		align = format.TextAlign
	}
	if align != "" {
		fmt.Fprintf(&builder, `<w:jc w:val="%s"/>`, align)
	}
	builder.WriteString(`</w:pPr>`)
	// Табуляция внутри текста - отдельный элемент w:tab
	for index, part := range strings.Split(text, "\t") {
		if index > 0 {
			builder.WriteString(`<w:r><w:tab/></w:r>`)
		}
		if part == "" {
			continue
		}
		builder.WriteString(`<w:r>`)
		if style.bold || style.small {
			builder.WriteString(`<w:rPr>`)
			if style.bold {
				builder.WriteString(`<w:b/>`)
			}
			if style.small {
				size := half_Points(format.SmallFontSizePt)
				fmt.Fprintf(&builder, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, size, size)
			}
			builder.WriteString(`</w:rPr>`)
		}
		builder.WriteString(`<w:t xml:space="preserve">`)
		builder.WriteString(escape_XML(part))
		builder.WriteString(`</w:t></w:r>`)
	}
	builder.WriteString(`</w:p>`)
	return builder.String()
}

// Размер шрифта в Word - в половинах пункта
func half_Points(points float64) int {
	return int(points*2 + 0.5)
}

func line_Spacing(lines float64) int {
	return int(lines*lineSpacingUnits + 0.5)
}

func (writer *docxWriter) empty() {
	writer.body.WriteString(`<w:p/>`)
}

// Картинка в отдельном абзаце. Размер - по пропорциям файла в пределах ширины и высоты
func (writer *docxWriter) image_XML(image *Image, name string, maxWidthMM, maxHeightMM float64, align string) string {
	widthMM, heightMM := fit_ImageSize(image.Body, maxWidthMM, maxHeightMM)
	if widthMM == 0 {
		return ""
	}
	index := len(writer.images) + 1
	id := fmt.Sprintf("rIdImage%d", index)
	extension := "png"
	if image.ContentType == "image/jpeg" {
		extension = "jpeg"
	}
	writer.images = append(writer.images, docxImage{id: id, name: fmt.Sprintf("image%d.%s", index, extension), contentType: image.ContentType, body: image.Body})
	cx, cy := int(widthMM*emuPerMM), int(heightMM*emuPerMM)
	return fmt.Sprintf(`<w:p><w:pPr><w:jc w:val="%s"/></w:pPr><w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0">`+
		`<wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="%s"/>`+
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic>`+
		`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`,
		align, cx, cy, index, escape_XML(name), index, escape_XML(name), id, cx, cy)
}

func fit_ImageSize(body []byte, maxWidthMM, maxHeightMM float64) (float64, float64) {
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || config.Width == 0 || config.Height == 0 {
		return 0, 0
	}
	ratio := float64(config.Width) / float64(config.Height)
	heightMM := maxHeightMM
	widthMM := heightMM * ratio
	if widthMM > maxWidthMM {
		widthMM = maxWidthMM
		heightMM = widthMM / ratio
	}
	return widthMM, heightMM
}

// Таблица без рамок из двух колонок: угловой бланк письма - слева реквизиты организации, справа адресат
func (writer *docxWriter) two_ColumnsXML(left, right string) string {
	half := twips(writer.format.text_WidthMM() / 2)
	cell := func(content string) string {
		if content == "" {
			content = `<w:p/>`
		}
		return fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr>%s</w:tc>`, half, content)
	}
	borders := `<w:tblBorders><w:top w:val="nil"/><w:left w:val="nil"/><w:bottom w:val="nil"/><w:right w:val="nil"/><w:insideH w:val="nil"/><w:insideV w:val="nil"/></w:tblBorders>`
	return fmt.Sprintf(`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblLayout w:type="fixed"/>%s`+
		`<w:tblCellMar><w:left w:w="0" w:type="dxa"/><w:right w:w="0" w:type="dxa"/></w:tblCellMar></w:tblPr>`+
		`<w:tblGrid><w:gridCol w:w="%d"/><w:gridCol w:w="%d"/></w:tblGrid><w:tr>%s%s</w:tr></w:tbl>`,
		half*2, borders, half, half, cell(left), cell(right))
}

func build_Docx(doc document, format Format) ([]byte, error) {
	writer := &docxWriter{format: format}
	textWidthMM := format.text_WidthMM()
	// Адресат в правой зоне с выравниванием по левому краю зоны (п. 8.12 Инструкции)
	rightZone := paragraphStyle{indentMM: textWidthMM - format.AddresseeZoneMM}
	switch doc.Kind {
	case texts.LayoutKindLetter:
		var left strings.Builder
		if doc.Emblem != nil {
			left.WriteString(writer.image_XML(doc.Emblem, "emblem", textWidthMM/2, format.EmblemHeightMM, "center"))
		}
		if doc.Stamp != nil {
			left.WriteString(writer.image_XML(doc.Stamp, "stamp", format.StampMaxWidthMM, format.StampMaxHeightMM, "center"))
		}
		left.WriteString(writer.paragraph_XML(doc.DateLine, paragraphStyle{align: "center", spaceMM: format.LetterDateSpaceMM}))
		var right strings.Builder
		for _, line := range doc.HeaderRight {
			right.WriteString(writer.paragraph_XML(line, paragraphStyle{indentMM: format.LetterAddresseeIndentMM}))
		}
		writer.body.WriteString(writer.two_ColumnsXML(left.String(), right.String()))
	case texts.LayoutKindAct, texts.LayoutKindProtocol:
		// Общий бланк: герб посередине над реквизитами организации (п. 5.2, 6.5 ГОСТ)
		if doc.Emblem != nil {
			writer.body.WriteString(writer.image_XML(doc.Emblem, "emblem", textWidthMM, format.EmblemHeightMM, "center"))
		}
		if doc.Stamp != nil {
			writer.body.WriteString(writer.image_XML(doc.Stamp, "stamp", format.StampMaxWidthMM, format.StampMaxHeightMM, "center"))
		}
		writer.paragraph(strings.ToUpper(doc.TypeTitle), paragraphStyle{align: "center", bold: true, spaceMM: format.TypeTitleSpaceMM})
		writer.paragraph(doc.DateLine, paragraphStyle{spaceMM: format.DateLineSpaceMM})
		if doc.Place != "" {
			writer.paragraph(doc.Place, paragraphStyle{})
		}
	default:
		for _, line := range doc.HeaderRight {
			if line == "" {
				writer.empty()
				continue
			}
			writer.paragraph(line, rightZone)
		}
		writer.empty()
		writer.paragraph(strings.ToUpper(doc.TypeTitle), paragraphStyle{bold: true})
		writer.paragraph(doc.DateLine, paragraphStyle{})
	}
	if doc.Title != "" {
		writer.paragraph(doc.Title, paragraphStyle{spaceMM: format.TitleSpaceMM})
	}
	writer.empty()
	for _, line := range doc.Preamble {
		writer.paragraph(line, paragraphStyle{bodyText: true})
	}
	if doc.Greeting != "" {
		writer.paragraph(doc.Greeting, paragraphStyle{align: "center", spaceMM: format.GreetingSpaceMM})
		writer.empty()
	}
	for _, line := range doc.Text {
		writer.paragraph(line, paragraphStyle{bodyText: true})
	}
	if doc.Attach != "" {
		writer.empty()
		writer.paragraph(texts.LayoutAttachLabel+" "+doc.Attach, paragraphStyle{})
	}
	if doc.Closing != "" {
		writer.empty()
		writer.paragraph(doc.Closing, paragraphStyle{})
	}
	for index, sign := range doc.Signatures {
		space := format.SignatureGapMM
		if index == 0 {
			space = format.FirstSignatureSpaceMM
		}
		writer.paragraph(sign.Position+"\t"+sign.Name, paragraphStyle{tabRight: true, spaceMM: space})
	}
	if doc.Executor != "" {
		if format.ExecutorAtPageBottom {
			writer.paragraph(doc.Executor, paragraphStyle{small: true, pageBottom: true})
		} else {
			writer.paragraph(doc.Executor, paragraphStyle{small: true, spaceMM: format.ExecutorSpaceMM})
		}
	}
	return writer.package_Docx()
}

// Колонтитул с номером страницы (поле PAGE) и пустой - для первой страницы, если ее не нумеруют
func (writer *docxWriter) page_NumberParts() (numbered, first string) {
	tag := "w:hdr"
	if writer.format.PageNumberPart == PageNumberFooter {
		tag = "w:ftr"
	}
	open := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><` + tag + ` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`
	numbered = open + fmt.Sprintf(`<w:p><w:pPr><w:jc w:val="%s"/></w:pPr>`, writer.format.PageNumberAlign) +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>2</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></` + tag + `>`
	first = open + `<w:p/></` + tag + `>`
	return numbered, first
}

func (writer *docxWriter) package_Docx() ([]byte, error) {
	format := writer.format
	partKind := "header"
	if format.PageNumberPart == PageNumberFooter {
		partKind = "footer"
	}
	numberedXML, firstXML := writer.page_NumberParts()
	references := fmt.Sprintf(`<w:%sReference w:type="default" r:id="%s"/>`, partKind, pageNumberRelation)
	titlePage := ""
	if !format.PageNumberOnFirstPage {
		references += fmt.Sprintf(`<w:%sReference w:type="first" r:id="%s"/>`, partKind, pageFirstRelation)
		titlePage = `<w:titlePg/>`
	}
	distance := twips(format.HeaderFooterDistanceMM)
	section := fmt.Sprintf(`<w:sectPr>%s<w:pgSz w:w="%d" w:h="%d"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="%d" w:footer="%d" w:gutter="0"/>%s</w:sectPr>`,
		references, twips(format.PageWidthMM), twips(format.PageHeightMM),
		twips(format.MarginTopMM), twips(format.MarginRightMM), twips(format.MarginBottomMM), twips(format.MarginLeftMM),
		distance, distance, titlePage)
	documentXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">` +
		`<w:body>` + writer.body.String() + section + `</w:body></w:document>`
	var relations strings.Builder
	relations.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	relations.WriteString(`<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	fmt.Fprintf(&relations, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/%s" Target="%s1.xml"/>`, pageNumberRelation, partKind, partKind)
	fmt.Fprintf(&relations, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/%s" Target="%s2.xml"/>`, pageFirstRelation, partKind, partKind)
	for _, item := range writer.images {
		fmt.Fprintf(&relations, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/%s"/>`, item.id, item.name)
	}
	relations.WriteString(`</Relationships>`)
	files := []struct {
		name string
		body []byte
	}{
		{"[Content_Types].xml", []byte(content_TypesXML(partKind))},
		{"_rels/.rels", []byte(packageRelsXML)},
		{"word/document.xml", []byte(documentXML)},
		{"word/styles.xml", []byte(styles_XML(format))},
		{"word/_rels/document.xml.rels", []byte(relations.String())},
		{"word/" + partKind + "1.xml", []byte(numberedXML)},
		{"word/" + partKind + "2.xml", []byte(firstXML)},
	}
	for _, item := range writer.images {
		files = append(files, struct {
			name string
			body []byte
		}{"word/media/" + item.name, item.body})
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, file := range files {
		entry, err := archive.Create(file.name)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(entry, bytes.NewReader(file.body)); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Шрифт и интервал реквизитов по умолчанию. Текст документа получает свой интервал в абзаце
func styles_XML(format Format) string {
	size := half_Points(format.FontSizePt)
	font := escape_XML(format.FontName)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults>`+
		`<w:rPrDefault><w:rPr><w:rFonts w:ascii="%s" w:hAnsi="%s" w:cs="%s" w:eastAsia="%s"/>`+
		`<w:sz w:val="%d"/><w:szCs w:val="%d"/><w:lang w:val="ru-RU"/></w:rPr></w:rPrDefault>`+
		`<w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr></w:pPrDefault>`+
		`</w:docDefaults></w:styles>`, font, font, font, font, size, size, line_Spacing(format.RequisiteLineSpacing))
}

func escape_XML(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

func content_TypesXML(partKind string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Default Extension="png" ContentType="image/png"/>
<Default Extension="jpeg" ContentType="image/jpeg"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/` + partKind + `1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.` + partKind + `+xml"/>
<Override PartName="/word/` + partKind + `2.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.` + partKind + `+xml"/>
</Types>`
}

const packageRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`
