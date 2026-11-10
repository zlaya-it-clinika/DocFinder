package formatcheck

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"docfinder/core/internal/texts"
)

// Реквизиты внешнего документа: адресат, заголовок к тексту, подпись. Если их нет,
// проверка об этом говорит, а исправление добавляет их по ответам сотрудника

const (
	RequisiteAddressee = "addressee"
	RequisiteTitle     = "title"
	RequisiteSignature = "signature"
	// Сколько первых и последних абзацев смотреть в поиске реквизитов
	requisiteHeadParagraphs = 15
	requisiteTailParagraphs = 5
	// Заголовок к тексту - короткая строка
	titleMaxRunes = 200
	// Строка подписи или адресата короче основного текста
	requisiteMaxRunes = 150
)

var (
	// «И. И. Иванов», «И.И. Иванову», «Иванов И.И.»
	personName = regexp.MustCompile(`[А-ЯЁ]\.\s?[А-ЯЁ]\.\s?[А-ЯЁ][а-яё]+|[А-ЯЁ][а-яё]+\s[А-ЯЁ]\.\s?[А-ЯЁ]\.`)
	titleStart = regexp.MustCompile(`^Об?\s`)
)

// Каких реквизитов нет: ключи RequisiteAddressee, RequisiteTitle, RequisiteSignature
func Find_MissingRequisites(body []byte) ([]string, error) {
	info, err := read_Document(body)
	if err != nil {
		return nil, err
	}
	return missing_Requisites(info), nil
}

func missing_Requisites(info documentInfo) []string {
	filled := []paragraphInfo{}
	for _, paragraph := range info.Paragraphs {
		if strings.TrimSpace(paragraph.Text) != "" {
			filled = append(filled, paragraph)
		}
	}
	titleIndex := -1
	for index, paragraph := range filled {
		if index >= requisiteHeadParagraphs {
			break
		}
		text := strings.TrimSpace(paragraph.Text)
		if titleStart.MatchString(text) && utf8.RuneCountInString(text) <= titleMaxRunes && !strings.HasSuffix(text, ".") {
			titleIndex = index
			break
		}
	}
	// Адресат - строки с ФИО до заголовка: в правой зоне, справа или в бланке-таблице
	headEnd := titleIndex
	if headEnd < 0 {
		headEnd = min(len(filled), requisiteHeadParagraphs/2)
	}
	hasAddressee := false
	for _, paragraph := range filled[:headEnd] {
		inZone := paragraph.InTable || paragraph.LeftMM > addresseeIndentMM || paragraph.Align == "right" || paragraph.Align == "end"
		if inZone && personName.MatchString(paragraph.Text) {
			hasAddressee = true
		}
	}
	hasSignature := false
	for index := max(0, len(filled)-requisiteTailParagraphs); index < len(filled); index++ {
		text := strings.TrimSpace(filled[index].Text)
		if utf8.RuneCountInString(text) <= requisiteMaxRunes && personName.MatchString(text) && !executorLine.MatchString(text) {
			hasSignature = true
		}
	}
	missing := []string{}
	if !hasAddressee {
		missing = append(missing, RequisiteAddressee)
	}
	if titleIndex < 0 {
		missing = append(missing, RequisiteTitle)
	}
	if !hasSignature {
		missing = append(missing, RequisiteSignature)
	}
	return missing
}

// Нет заголовка или подписи - замечание. Адресат нужен не всякому документу (приказ, акт), его только предлагают добавить
func (checker *checker) check_Requisites(info documentInfo) {
	for _, key := range missing_Requisites(info) {
		switch key {
		case RequisiteTitle:
			checker.add_WithRef(texts.FormatLevelRemark, texts.FormatTitleProfile, texts.FormatTitleRef, texts.FormatTitleMissing, nil)
		case RequisiteSignature:
			checker.add(texts.FormatLevelRemark, "signatureNameOrder", texts.FormatSignatureMissing, nil)
		}
	}
}

// Что добавить при исправлении. Пустое поле - реквизит не добавляется
type FixOptions struct {
	Addressee []string
	Title     string
	// Подпись: должность и инициалы с фамилией
	SignerPosition string
	SignerName     string
}

func (fixer *fixer) run_Props() string {
	format := fixer.format
	props := ""
	if font := format.FontName; font != "" {
		props += `<w:rFonts w:ascii="` + escape_Text(font) + `" w:hAnsi="` + escape_Text(font) + `" w:cs="` + escape_Text(font) + `" w:eastAsia="` + escape_Text(font) + `"/>`
	}
	if format.FontSizePt > 0 {
		size := strconv.Itoa(int(math.Round(format.FontSizePt * 2)))
		props += `<w:sz w:val="` + size + `"/><w:szCs w:val="` + size + `"/>`
	}
	return `<w:rPr>` + props + `</w:rPr>`
}

func (fixer *fixer) requisite_Paragraph(paragraphProps string, runs ...string) string {
	var builder strings.Builder
	builder.WriteString(`<w:p><w:pPr>` + paragraphProps + fixer.run_Props() + `</w:pPr>`)
	for index, text := range runs {
		if index > 0 {
			builder.WriteString(`<w:r>` + fixer.run_Props() + `<w:tab/></w:r>`)
		}
		builder.WriteString(`<w:r>` + fixer.run_Props() + `<w:t xml:space="preserve">` + escape_Text(text) + `</w:t></w:r>`)
	}
	builder.WriteString(`</w:p>`)
	return builder.String()
}

func (fixer *fixer) single_Spacing(afterMM float64, beforeMM float64) string {
	line := 240
	if fixer.format.RequisiteLineSpacing > 0 {
		line = int(math.Round(fixer.format.RequisiteLineSpacing * 240))
	}
	return `<w:spacing w:before="` + twips(beforeMM) + `" w:after="` + twips(afterMM) + `" w:line="` + strconv.Itoa(line) + `" w:lineRule="auto"/>`
}

// Адресат в правой зоне листа шириной addresseeZoneMM (п. 5.15 ГОСТ), за ним пустая строка
func (fixer *fixer) addressee_XML(lines []string) string {
	format := fixer.format
	indent := format.Text_WidthMM() - format.AddresseeZoneMM
	if indent < 0 {
		indent = 0
	}
	var builder strings.Builder
	for index, line := range lines {
		after := 0.0
		if index == len(lines)-1 {
			after = format.TitleSpaceMM
		}
		builder.WriteString(fixer.requisite_Paragraph(fixer.single_Spacing(after, 0)+`<w:ind w:left="`+twips(indent)+`" w:firstLine="0"/><w:jc w:val="left"/>`, line))
	}
	return builder.String()
}

// Заголовок к тексту слева, без абзацного отступа, отбивка до текста
func (fixer *fixer) title_XML(title string) string {
	return fixer.requisite_Paragraph(fixer.single_Spacing(fixer.format.TitleSpaceMM, 0)+`<w:ind w:firstLine="0"/><w:jc w:val="left"/>`, title)
}

// Подпись: должность слева, инициалы и фамилия у правого поля по табуляции
func (fixer *fixer) signature_XML(position, name string) string {
	width := twips(fixer.format.Text_WidthMM())
	props := `<w:tabs><w:tab w:val="right" w:pos="` + width + `"/></w:tabs>` + fixer.single_Spacing(0, fixer.format.FirstSignatureSpaceMM) + `<w:ind w:firstLine="0"/><w:jc w:val="left"/>`
	if strings.TrimSpace(position) == "" {
		return fixer.requisite_Paragraph(props, "", name)
	}
	return fixer.requisite_Paragraph(props, position, name)
}

func escape_Text(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(text)
}
