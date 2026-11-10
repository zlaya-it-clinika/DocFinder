package formatcheck

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"docfinder/core/internal/layout"
	"docfinder/core/internal/texts"
)

// Проверка оформления .docx по правилам из профилей оформления (Инструкция по ДОУ, затем ГОСТ Р 7.0.97-2025).
// У каждого замечания - правило, откуда оно взято: профиль и пункт

type Finding struct {
	// texts.FormatLevelError - нарушение правила, texts.FormatLevelRemark - стоит проверить
	Level   string
	Text    string
	Profile string
	Ref     string
	// Номер пункта из ссылки правила: «8.16» из «п. 8.16: ...»
	Clause string
}

var (
	clauseNumber   = regexp.MustCompile(`пп?\.\s*(\d+(?:\.\d+)*)`)
	dateQuoted     = regexp.MustCompile(`«\s*\d{1,2}\s*»\s*[а-яё]+\s+\d{4}`)
	dateShort      = regexp.MustCompile(`(^|[^\d.])(\d{1,2}\.\d{1,2}\.\d{2,4})($|[^\d.])`)
	initialsTight  = regexp.MustCompile(`(^|[^\p{L}])([А-ЯЁ]\.[А-ЯЁ]\.\s?[А-ЯЁ][а-яё]+)`)
	initialsSpaced = regexp.MustCompile(`(^|[^\p{L}])([А-ЯЁ]\.\s+[А-ЯЁ]\.\s?[А-ЯЁ][а-яё]+)`)
	templateLines  = regexp.MustCompile(`_{3,}`)
	// «№ ______» в реквизите даты и номера - место для номера при регистрации, это не остаток шаблона
	numberLine = regexp.MustCompile(`№\s*_{3,}`)
	// Предложение заканчивается точкой, знаком вопроса или восклицания, многоточием, «;» или «:»
	sentenceEnd = regexp.MustCompile(`[.!?;:…»)"]$`)
	// Отметка об исполнителе: «Исп.», «Исполнитель» или ФИО с телефоном
	executorLine    = regexp.MustCompile(`(?i)^(исп\.|исполнитель)|^\S+(\s\S+){0,3},?\s*(тел\.?\s*)?\+?[78]?\s?\(?\d{3,4}\)?[\s-]?\d{2,3}-?\d{2}-?\d{2}$`)
	templateMarks   = regexp.MustCompile(`\[\d+\]`)
	templateColors  = map[string]bool{"FF0000": true, "C00000": true, "EE0000": true}
	alignTitles     = map[string]string{"left": texts.FormatAlignLeft, "start": texts.FormatAlignLeft, "right": texts.FormatAlignRight, "end": texts.FormatAlignRight, "center": texts.FormatAlignCenter}
	partTitles      = map[string]string{"header": texts.FormatPartHeader, "footer": texts.FormatPartFooter}
	justifiedAligns = map[string]bool{"both": true, "distribute": true}
)

const (
	// Погрешности сравнения: значения из Word пересчитываются из твипов
	marginToleranceMM  = 0.5
	pageToleranceMM    = 2
	indentToleranceMM  = 1.5
	spacingTolerance   = 0.01
	maxTextLineSpacing = 1.5
	// Числа пишутся с десятичной запятой, поэтому список значений - через точку с запятой
	valueSeparator = "; "
	// Левый отступ больше этого - строка в правой зоне листа (адресат), не основной текст
	addresseeIndentMM = 40
)

func Check_Docx(body []byte, format layout.Format) ([]Finding, error) {
	info, err := read_Document(body)
	if err != nil {
		return nil, err
	}
	checker := &checker{format: format}
	checker.check_Page(info)
	checker.check_Fonts(info)
	checker.check_BodyText(info)
	checker.check_PageNumbers(info)
	checker.check_Text(info)
	checker.check_Template(info)
	checker.check_Requisites(info)
	// Нарушения правил первыми, затем замечания, внутри - в порядке проверок
	sort.SliceStable(checker.findings, func(left, right int) bool {
		return checker.findings[left].Level == texts.FormatLevelError && checker.findings[right].Level != texts.FormatLevelError
	})
	return checker.findings, nil
}

type checker struct {
	format   layout.Format
	findings []Finding
}

func (checker *checker) add(level, rule, template string, values map[string]string) {
	ref := checker.format.Refs[rule]
	checker.add_WithRef(level, ref.Profile, ref.Text, template, values)
}

func (checker *checker) add_WithRef(level, profile, ref, template string, values map[string]string) {
	text := template
	for key, value := range values {
		text = strings.ReplaceAll(text, "{"+key+"}", value)
	}
	clause := ""
	if match := clauseNumber.FindStringSubmatch(ref); match != nil {
		clause = match[1]
	}
	checker.findings = append(checker.findings, Finding{Level: level, Text: text, Profile: profile, Ref: ref, Clause: clause})
}

func (checker *checker) check_Page(info documentInfo) {
	format := checker.format
	if !info.HasSection {
		return
	}
	if info.PageWidthMM > 0 && info.PageHeightMM > 0 && format.PageWidthMM > 0 {
		portrait := near(info.PageWidthMM, format.PageWidthMM, pageToleranceMM) && near(info.PageHeightMM, format.PageHeightMM, pageToleranceMM)
		landscape := near(info.PageWidthMM, format.PageHeightMM, pageToleranceMM) && near(info.PageHeightMM, format.PageWidthMM, pageToleranceMM)
		if !portrait && !landscape {
			checker.add(texts.FormatLevelError, "pageWidthMM", texts.FormatPageSize, map[string]string{
				"width": format_MM(info.PageWidthMM), "height": format_MM(info.PageHeightMM),
				"needWidth": format_MM(format.PageWidthMM), "needHeight": format_MM(format.PageHeightMM),
			})
		}
	}
	margins := []struct {
		rule     string
		template string
		actual   float64
		need     float64
	}{
		{"marginLeftMM", texts.FormatMarginLeft, info.MarginLeftMM, format.MarginLeftMM},
		{"marginRightMM", texts.FormatMarginRight, info.MarginRightMM, format.MarginRightMM},
		{"marginTopMM", texts.FormatMarginTop, info.MarginTopMM, format.MarginTopMM},
		{"marginBottomMM", texts.FormatMarginBottom, info.MarginBottomMM, format.MarginBottomMM},
	}
	for _, margin := range margins {
		if margin.actual > 0 && margin.need > 0 && margin.actual < margin.need-marginToleranceMM {
			checker.add(texts.FormatLevelError, margin.rule, margin.template, map[string]string{"value": format_MM(margin.actual), "need": format_MM(margin.need)})
		}
	}
}

// Шрифт всех абзацев с текстом, включая таблицы
func (checker *checker) check_Fonts(info documentInfo) {
	need := strings.TrimSpace(checker.format.FontName)
	if need == "" {
		return
	}
	wrong := map[string]bool{}
	count := 0
	example := ""
	for _, paragraph := range info.Paragraphs {
		bad := false
		for _, run := range paragraph.Runs {
			if strings.TrimSpace(run.Text) == "" || run.Font == "" || strings.EqualFold(run.Font, need) {
				continue
			}
			wrong[run.Font] = true
			bad = true
		}
		if bad {
			count++
			if example == "" {
				example = short_Text(paragraph.Text)
			}
		}
	}
	if count > 0 {
		checker.add(texts.FormatLevelError, "fontName", texts.FormatFontName, map[string]string{
			"fonts": strings.Join(sorted_Keys(wrong), ", "), "need": need, "count": strconv.Itoa(count), "example": example,
		})
	}
}

// Основной текст: длинные абзацы вне таблиц. Короткие строки - реквизиты, у них свои правила
func (checker *checker) check_BodyText(info documentInfo) {
	format := checker.format
	sizes := map[string]bool{}
	spacings := map[string]bool{}
	indents := map[string]bool{}
	var sizeCount, spacingCount, alignCount, indentCount int
	var sizeExample, spacingExample, alignExample, indentExample string
	for _, paragraph := range info.Paragraphs {
		if !check_TextParagraph(paragraph) {
			continue
		}
		example := short_Text(paragraph.Text)
		if format.FontSizePt > 0 {
			badSize := false
			for _, run := range paragraph.Runs {
				if strings.TrimSpace(run.Text) == "" || run.SizePt == 0 {
					continue
				}
				if !near(run.SizePt, format.FontSizePt, 0.01) {
					sizes[format_Number(run.SizePt)] = true
					badSize = true
				}
			}
			if badSize {
				sizeCount++
				sizeExample = first_NonEmpty(sizeExample, example)
			}
		}
		// Интервал, выравнивание и отступ - только у абзацев основного текста, реквизиты и списки оформлены иначе
		if !check_BodyParagraph(paragraph) {
			continue
		}
		if format.TextLineSpacing > 0 && paragraph.LineSpacing > 0 &&
			(paragraph.LineSpacing < format.TextLineSpacing-spacingTolerance || paragraph.LineSpacing > maxTextLineSpacing+spacingTolerance) {
			spacings[format_Number(math.Round(paragraph.LineSpacing*100)/100)] = true
			spacingCount++
			spacingExample = first_NonEmpty(spacingExample, example)
		}
		if format.TextAlign == "both" && !justifiedAligns[paragraph.Align] {
			alignCount++
			alignExample = first_NonEmpty(alignExample, example)
		}
		if format.FirstLineIndentMM > 0 && math.Abs(paragraph.FirstLineMM-format.FirstLineIndentMM) > indentToleranceMM {
			indents[format_MM(paragraph.FirstLineMM)] = true
			indentCount++
			indentExample = first_NonEmpty(indentExample, example)
		}
	}
	if sizeCount > 0 {
		checker.add(texts.FormatLevelError, "fontSizePt", texts.FormatFontSize, map[string]string{
			"sizes": strings.Join(sorted_Keys(sizes), valueSeparator), "need": format_Number(format.FontSizePt), "count": strconv.Itoa(sizeCount), "example": sizeExample,
		})
	}
	if spacingCount > 0 {
		checker.add(texts.FormatLevelError, "textLineSpacing", texts.FormatLineSpacing, map[string]string{
			"values": strings.Join(sorted_Keys(spacings), valueSeparator), "min": format_Number(format.TextLineSpacing), "max": format_Number(maxTextLineSpacing),
			"count": strconv.Itoa(spacingCount), "example": spacingExample,
		})
	}
	if alignCount > 0 {
		checker.add(texts.FormatLevelError, "textAlign", texts.FormatAlign, map[string]string{"count": strconv.Itoa(alignCount), "example": alignExample})
	}
	if indentCount > 0 {
		checker.add(texts.FormatLevelRemark, "firstLineIndentMM", texts.FormatIndent, map[string]string{
			"values": strings.Join(sorted_Keys(indents), valueSeparator), "need": format_MM(format.FirstLineIndentMM), "count": strconv.Itoa(indentCount), "example": indentExample,
		})
	}
}

func (checker *checker) check_PageNumbers(info documentInfo) {
	format := checker.format
	if len(info.PageNumbers) == 0 {
		characters := 0
		for _, paragraph := range info.Paragraphs {
			characters += utf8.RuneCountInString(paragraph.Text)
		}
		if characters > texts.FormatPageRunes {
			checker.add(texts.FormatLevelRemark, "pageNumberPart", texts.FormatPageNumberNone, nil)
		}
		return
	}
	number := info.PageNumbers[0]
	if format.PageNumberPart != "" && number.Part != format.PageNumberPart {
		checker.add(texts.FormatLevelError, "pageNumberPart", texts.FormatPageNumberPart, map[string]string{
			"actual": partTitles[number.Part], "need": partTitles[format.PageNumberPart],
		})
	} else if format.PageNumberAlign != "" && number.Align != format.PageNumberAlign && !(number.Align == "" && format.PageNumberAlign == "left") {
		actual := alignTitles[number.Align]
		if actual == "" {
			actual = texts.FormatAlignLeft
		}
		checker.add(texts.FormatLevelError, "pageNumberAlign", texts.FormatPageNumberAlign, map[string]string{
			"actual": actual, "need": alignTitles[format.PageNumberAlign],
		})
	}
	if !format.PageNumberOnFirstPage && !info.TitlePage {
		checker.add(texts.FormatLevelError, "pageNumberOnFirstPage", texts.FormatPageNumberFirst, nil)
	}
}

// Даты и инициалы во всем тексте, включая таблицы
func (checker *checker) check_Text(info documentInfo) {
	format := checker.format
	need := strings.NewReplacer("DD", "ДД", "MM", "ММ", "YYYY", "ГГГГ").Replace(format.DateFormat)
	var quoted, short, tight, spaced string
	for _, paragraph := range info.Paragraphs {
		text := paragraph.Text
		if match := dateQuoted.FindString(text); match != "" && quoted == "" {
			quoted = match
		}
		if match := dateShort.FindStringSubmatch(text); match != nil && short == "" && check_ShortDate(match[2]) {
			short = match[2]
		}
		if match := initialsTight.FindStringSubmatch(text); match != nil && tight == "" {
			tight = match[2]
		}
		if match := initialsSpaced.FindStringSubmatch(text); match != nil && spaced == "" {
			spaced = match[2]
		}
	}
	if format.DateFormat != "" {
		if quoted != "" {
			checker.add(texts.FormatLevelError, "dateFormat", texts.FormatDateQuoted, map[string]string{"example": quoted, "need": need})
		}
		if short != "" {
			checker.add(texts.FormatLevelError, "dateFormat", texts.FormatDateShort, map[string]string{"example": short, "need": need})
		}
	}
	if format.InitialsSeparator == " " && tight != "" {
		checker.add(texts.FormatLevelRemark, "initialsSeparator", texts.FormatInitialsTight, map[string]string{"example": tight})
	}
	if format.InitialsSeparator == "" && spaced != "" {
		checker.add(texts.FormatLevelRemark, "initialsSeparator", texts.FormatInitialsSpaced, map[string]string{"example": spaced})
	}
}

// Остатки шаблона модульного документа
func (checker *checker) check_Template(info documentInfo) {
	var lines, marks, red string
	module := false
	for _, paragraph := range info.Paragraphs {
		if lines == "" && templateLines.MatchString(numberLine.ReplaceAllString(paragraph.Text, "")) {
			lines = short_Text(paragraph.Text)
		}
		if marks == "" && templateMarks.MatchString(paragraph.Text) {
			marks = short_Text(paragraph.Text)
		}
		if strings.Contains(strings.ToUpper(paragraph.Text), texts.FormatModuleMark) {
			module = true
		}
		for _, run := range paragraph.Runs {
			if red == "" && strings.TrimSpace(run.Text) != "" && templateColors[run.Color] {
				red = short_Text(run.Text)
			}
		}
	}
	add := func(level, template, example string) {
		checker.add_WithRef(level, texts.FormatTemplateProfile, texts.FormatTemplateRef, template, map[string]string{"example": example})
	}
	if red != "" {
		add(texts.FormatLevelError, texts.FormatTemplateRed, red)
	}
	if marks != "" {
		add(texts.FormatLevelError, texts.FormatTemplateMarks, marks)
	}
	if module {
		add(texts.FormatLevelError, texts.FormatTemplateModule, "")
	}
	// Линия бывает и местом для подписи от руки, поэтому только замечание
	if lines != "" {
		add(texts.FormatLevelRemark, texts.FormatTemplateLines, lines)
	}
}

// Абзац с текстом вне таблицы. Отметка об исполнителе набирается шрифтом меньше, ее размер не проверяется
func check_TextParagraph(paragraph paragraphInfo) bool {
	text := strings.TrimSpace(paragraph.Text)
	return !paragraph.InTable && text != "" && !executorLine.MatchString(text)
}

// Основной текст: предложение или длинный абзац. Не основной текст - заголовок прописными,
// строка по центру или справа, адресат в правой зоне, пункт списка, абзац с выступом
func check_BodyParagraph(paragraph paragraphInfo) bool {
	text := strings.TrimSpace(paragraph.Text)
	if !check_TextParagraph(paragraph) || check_UpperText(text) || paragraph.Numbered || paragraph.FirstLineMM < 0 || paragraph.LeftMM > addresseeIndentMM {
		return false
	}
	switch paragraph.Align {
	case "center", "right", "end":
		return false
	}
	return sentenceEnd.MatchString(text) || utf8.RuneCountInString(text) >= texts.FormatBodyParagraphRunes
}

// «5.6.2024», «05.6.2024» - день или месяц одной цифрой. Номера пунктов «3.2.1» сюда не попадают: в дате год из 2 или 4 цифр
func check_ShortDate(date string) bool {
	parts := strings.Split(date, ".")
	if len(parts) != 3 || (len(parts[2]) != 4 && len(parts[2]) != 2) {
		return false
	}
	day, dayErr := strconv.Atoi(parts[0])
	month, monthErr := strconv.Atoi(parts[1])
	if dayErr != nil || monthErr != nil || day < 1 || day > 31 || month < 1 || month > 12 || len(parts[2]) != 4 {
		return false
	}
	return len(parts[0]) == 1 || len(parts[1]) == 1
}

// Заголовок прописными («ПРИКАЗЫВАЮ:», вид документа) - не основной текст
func check_UpperText(text string) bool {
	letters, upper := 0, 0
	for _, symbol := range text {
		if unicode.IsLetter(symbol) {
			letters++
			if unicode.IsUpper(symbol) {
				upper++
			}
		}
	}
	return letters > 0 && upper*10 >= letters*9
}

func short_Text(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > texts.FormatExampleRunes {
		return string(runes[:texts.FormatExampleRunes]) + "…"
	}
	return text
}

func near(actual, need, tolerance float64) bool {
	return math.Abs(actual-need) <= tolerance
}

func format_MM(value float64) string {
	return format_Number(math.Round(value*10) / 10)
}

func format_Number(value float64) string {
	return strings.ReplaceAll(strconv.FormatFloat(value, 'f', -1, 64), ".", ",")
}

func sorted_Keys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (finding Finding) String() string {
	return fmt.Sprintf("[%s] %s (%s, %s)", finding.Level, finding.Text, finding.Profile, finding.Ref)
}
