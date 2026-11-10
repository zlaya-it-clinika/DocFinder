package formatcheck

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

// Разбор .docx для проверки оформления: параметры страницы, стили, абзацы с итоговым
// (с учетом стилей и значений по умолчанию) шрифтом, размером, интервалом, выравниванием и отступом.
// Теги без пространства имен: encoding/xml сравнивает только локальные имена

var ErrNotDocx = errors.New("not docx")

const twipsPerMM = 56.6929

type xVal struct {
	Val string `xml:"val,attr"`
}

type xFonts struct {
	Ascii      string `xml:"ascii,attr"`
	HAnsi      string `xml:"hAnsi,attr"`
	AsciiTheme string `xml:"asciiTheme,attr"`
}

type xRunProps struct {
	Fonts *xFonts `xml:"rFonts"`
	Size  *xVal   `xml:"sz"`
	Color *xVal   `xml:"color"`
	Style *xVal   `xml:"rStyle"`
}

type xSpacing struct {
	Line     string `xml:"line,attr"`
	LineRule string `xml:"lineRule,attr"`
}

type xIndent struct {
	FirstLine string `xml:"firstLine,attr"`
	Hanging   string `xml:"hanging,attr"`
	Left      string `xml:"left,attr"`
	Start     string `xml:"start,attr"`
}

type xParagraphProps struct {
	Style     *xVal      `xml:"pStyle"`
	Align     *xVal      `xml:"jc"`
	Spacing   *xSpacing  `xml:"spacing"`
	Indent    *xIndent   `xml:"ind"`
	Run       *xRunProps `xml:"rPr"`
	Section   *xSection  `xml:"sectPr"`
	Numbering *struct{}  `xml:"numPr"`
}

type xRun struct {
	Props *xRunProps `xml:"rPr"`
	Texts []string   `xml:"t"`
	Tabs  []struct{} `xml:"tab"`
	Instr []string   `xml:"instrText"`
}

type xSimpleField struct {
	Instr string `xml:"instr,attr"`
	Runs  []xRun `xml:"r"`
}

type xParagraph struct {
	Props      *xParagraphProps `xml:"pPr"`
	Runs       []xRun           `xml:"r"`
	Hyperlinks []struct {
		Runs []xRun `xml:"r"`
	} `xml:"hyperlink"`
	Fields []xSimpleField `xml:"fldSimple"`
}

type xCell struct {
	Paragraphs []xParagraph `xml:"p"`
	Tables     []xTable     `xml:"tbl"`
}

type xTable struct {
	Rows []struct {
		Cells []xCell `xml:"tc"`
	} `xml:"tr"`
}

type xPageSize struct {
	W string `xml:"w,attr"`
	H string `xml:"h,attr"`
}

type xPageMargin struct {
	Left   string `xml:"left,attr"`
	Right  string `xml:"right,attr"`
	Top    string `xml:"top,attr"`
	Bottom string `xml:"bottom,attr"`
}

type xOnOff struct {
	Val *string `xml:"val,attr"`
}

type xSection struct {
	PageSize   *xPageSize   `xml:"pgSz"`
	PageMargin *xPageMargin `xml:"pgMar"`
	TitlePage  *xOnOff      `xml:"titlePg"`
}

// Абзацы и таблицы тела в порядке документа
type xBody struct {
	Items   []xBodyItem
	Section *xSection
}

type xBodyItem struct {
	Paragraph *xParagraph
	Table     *xTable
}

func (body *xBody) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "p":
				var paragraph xParagraph
				if err := decoder.DecodeElement(&paragraph, &element); err != nil {
					return err
				}
				body.Items = append(body.Items, xBodyItem{Paragraph: &paragraph})
			case "tbl":
				var table xTable
				if err := decoder.DecodeElement(&table, &element); err != nil {
					return err
				}
				body.Items = append(body.Items, xBodyItem{Table: &table})
			case "sectPr":
				var section xSection
				if err := decoder.DecodeElement(&section, &element); err != nil {
					return err
				}
				body.Section = &section
			default:
				if err := decoder.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			return nil
		}
	}
}

type xDocument struct {
	Body xBody `xml:"body"`
}

type xStyle struct {
	Type      string           `xml:"type,attr"`
	StyleID   string           `xml:"styleId,attr"`
	Default   string           `xml:"default,attr"`
	BasedOn   *xVal            `xml:"basedOn"`
	Paragraph *xParagraphProps `xml:"pPr"`
	Run       *xRunProps       `xml:"rPr"`
}

type xStyles struct {
	Defaults struct {
		Run struct {
			Props *xRunProps `xml:"rPr"`
		} `xml:"rPrDefault"`
		Paragraph struct {
			Props *xParagraphProps `xml:"pPr"`
		} `xml:"pPrDefault"`
	} `xml:"docDefaults"`
	Styles []xStyle `xml:"style"`
}

type xThemeFont struct {
	Latin struct {
		Typeface string `xml:"typeface,attr"`
	} `xml:"latin"`
}

type xTheme struct {
	Fonts struct {
		Scheme struct {
			Major xThemeFont `xml:"majorFont"`
			Minor xThemeFont `xml:"minorFont"`
		} `xml:"fontScheme"`
	} `xml:"themeElements"`
}

// Абзац с итоговым оформлением
type paragraphInfo struct {
	Text    string
	InTable bool
	// Итоговые значения. Пустая строка или 0 - неизвестно
	Align       string
	LineSpacing float64
	FirstLineMM float64
	HasIndent   bool
	// Левый отступ абзаца: адресат в правой зоне листа
	LeftMM float64
	// Пункт нумерованного или маркированного списка
	Numbered bool
	Runs     []runInfo
}

type runInfo struct {
	Text   string
	Font   string
	SizePt float64
	Color  string
}

// Номер страницы в колонтитуле
type pageNumberInfo struct {
	Part  string
	Align string
}

type documentInfo struct {
	PageWidthMM    float64
	PageHeightMM   float64
	MarginLeftMM   float64
	MarginRightMM  float64
	MarginTopMM    float64
	MarginBottomMM float64
	HasSection     bool
	TitlePage      bool
	Paragraphs     []paragraphInfo
	PageNumbers    []pageNumberInfo
}

type styleSheet struct {
	byID          map[string]xStyle
	defaultRun    *xRunProps
	defaultPara   *xParagraphProps
	defaultParaID string
	majorFont     string
	minorFont     string
}

func read_Document(body []byte) (documentInfo, error) {
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return documentInfo{}, ErrNotDocx
	}
	files := map[string][]byte{}
	for _, entry := range archive.File {
		name := entry.Name
		if name != "word/document.xml" && name != "word/styles.xml" && name != "word/theme/theme1.xml" &&
			!(strings.HasPrefix(name, "word/header") || strings.HasPrefix(name, "word/footer")) {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return documentInfo{}, ErrNotDocx
		}
		raw, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return documentInfo{}, ErrNotDocx
		}
		files[name] = raw
	}
	raw, found := files["word/document.xml"]
	if !found {
		return documentInfo{}, ErrNotDocx
	}
	var document xDocument
	if err := xml.Unmarshal(raw, &document); err != nil {
		return documentInfo{}, ErrNotDocx
	}
	sheet := read_StyleSheet(files["word/styles.xml"], files["word/theme/theme1.xml"])
	info := documentInfo{}
	if section := document.Body.Section; section != nil {
		info.HasSection = true
		if section.PageSize != nil {
			info.PageWidthMM = twips_ToMM(section.PageSize.W)
			info.PageHeightMM = twips_ToMM(section.PageSize.H)
		}
		if margin := section.PageMargin; margin != nil {
			info.MarginLeftMM = twips_ToMM(margin.Left)
			info.MarginRightMM = twips_ToMM(margin.Right)
			info.MarginTopMM = twips_ToMM(margin.Top)
			info.MarginBottomMM = twips_ToMM(margin.Bottom)
		}
		info.TitlePage = check_OnOff(section.TitlePage)
	}
	for _, item := range document.Body.Items {
		if item.Paragraph != nil {
			info.Paragraphs = append(info.Paragraphs, sheet.resolve_Paragraph(*item.Paragraph, false))
		}
		if item.Table != nil {
			info.Paragraphs = append(info.Paragraphs, sheet.table_Paragraphs(*item.Table)...)
		}
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
		info.PageNumbers = append(info.PageNumbers, find_PageNumbers(raw, part, sheet)...)
	}
	return info, nil
}

func (sheet styleSheet) table_Paragraphs(table xTable) []paragraphInfo {
	paragraphs := []paragraphInfo{}
	for _, row := range table.Rows {
		for _, cell := range row.Cells {
			for _, paragraph := range cell.Paragraphs {
				paragraphs = append(paragraphs, sheet.resolve_Paragraph(paragraph, true))
			}
			for _, nested := range cell.Tables {
				paragraphs = append(paragraphs, sheet.table_Paragraphs(nested)...)
			}
		}
	}
	return paragraphs
}

// Колонтитул с полем PAGE: где он и как выровнен
func find_PageNumbers(raw []byte, part string, sheet styleSheet) []pageNumberInfo {
	var container struct {
		Paragraphs []xParagraph `xml:"p"`
		Tables     []xTable     `xml:"tbl"`
	}
	if err := xml.Unmarshal(raw, &container); err != nil {
		return nil
	}
	found := []pageNumberInfo{}
	for _, paragraph := range container.Paragraphs {
		if !check_HasPageField(paragraph) {
			continue
		}
		resolved := sheet.resolve_Paragraph(paragraph, false)
		found = append(found, pageNumberInfo{Part: part, Align: resolved.Align})
	}
	return found
}

func check_HasPageField(paragraph xParagraph) bool {
	for _, field := range paragraph.Fields {
		if strings.Contains(strings.ToUpper(field.Instr), "PAGE") && !strings.Contains(strings.ToUpper(field.Instr), "NUMPAGES") {
			return true
		}
	}
	for _, run := range paragraph.Runs {
		for _, instr := range run.Instr {
			upper := strings.ToUpper(strings.TrimSpace(instr))
			if strings.HasPrefix(upper, "PAGE") && !strings.HasPrefix(upper, "PAGES") {
				return true
			}
		}
	}
	return false
}

func read_StyleSheet(stylesRaw, themeRaw []byte) styleSheet {
	sheet := styleSheet{byID: map[string]xStyle{}}
	var styles xStyles
	if len(stylesRaw) > 0 && xml.Unmarshal(stylesRaw, &styles) == nil {
		sheet.defaultRun = styles.Defaults.Run.Props
		sheet.defaultPara = styles.Defaults.Paragraph.Props
		for _, style := range styles.Styles {
			sheet.byID[style.StyleID] = style
			if style.Type == "paragraph" && check_True(style.Default) {
				sheet.defaultParaID = style.StyleID
			}
		}
	}
	var theme xTheme
	if len(themeRaw) > 0 && xml.Unmarshal(themeRaw, &theme) == nil {
		sheet.majorFont = theme.Fonts.Scheme.Major.Latin.Typeface
		sheet.minorFont = theme.Fonts.Scheme.Minor.Latin.Typeface
	}
	return sheet
}

// Цепочка стиля от базового к самому стилю: свойства потомка перекрывают базовые
func (sheet styleSheet) style_Chain(styleID string) []xStyle {
	chain := []xStyle{}
	seen := map[string]bool{}
	for styleID != "" && !seen[styleID] {
		seen[styleID] = true
		style, found := sheet.byID[styleID]
		if !found {
			break
		}
		chain = append([]xStyle{style}, chain...)
		if style.BasedOn == nil {
			break
		}
		styleID = style.BasedOn.Val
	}
	return chain
}

type runState struct {
	font  string
	theme string
	size  float64
	color string
}

func (state *runState) apply(props *xRunProps) {
	if props == nil {
		return
	}
	if props.Fonts != nil {
		if font := first_NonEmpty(props.Fonts.Ascii, props.Fonts.HAnsi); font != "" {
			state.font, state.theme = font, ""
		} else if props.Fonts.AsciiTheme != "" {
			state.font, state.theme = "", props.Fonts.AsciiTheme
		}
	}
	if props.Size != nil {
		if halfPoints, err := strconv.ParseFloat(props.Size.Val, 64); err == nil {
			state.size = halfPoints / 2
		}
	}
	if props.Color != nil {
		state.color = strings.ToUpper(props.Color.Val)
	}
}

type paragraphState struct {
	align       string
	lineSpacing float64
	firstLineMM float64
	hasIndent   bool
	leftMM      float64
	numbered    bool
}

func (state *paragraphState) apply(props *xParagraphProps) {
	if props == nil {
		return
	}
	if props.Align != nil {
		state.align = props.Align.Val
	}
	if props.Spacing != nil && props.Spacing.Line != "" {
		rule := props.Spacing.LineRule
		if rule == "" || rule == "auto" {
			if line, err := strconv.ParseFloat(props.Spacing.Line, 64); err == nil {
				state.lineSpacing = line / 240
			}
		} else {
			// Точный или минимальный интервал в пунктах множителем не выражается
			state.lineSpacing = 0
		}
	}
	if props.Numbering != nil {
		state.numbered = true
	}
	if props.Indent != nil {
		if left := first_NonEmpty(props.Indent.Left, props.Indent.Start); left != "" {
			state.leftMM = twips_ToMM(left)
		}
		if props.Indent.FirstLine != "" {
			state.firstLineMM = twips_ToMM(props.Indent.FirstLine)
			state.hasIndent = true
		}
		if props.Indent.Hanging != "" {
			state.firstLineMM = -twips_ToMM(props.Indent.Hanging)
			state.hasIndent = true
		}
	}
}

func (sheet styleSheet) resolve_Paragraph(paragraph xParagraph, inTable bool) paragraphInfo {
	paraState := paragraphState{lineSpacing: 1}
	baseRun := runState{size: 10}
	baseRun.apply(sheet.defaultRun)
	paraState.apply(sheet.defaultPara)
	styleID := sheet.defaultParaID
	if paragraph.Props != nil && paragraph.Props.Style != nil {
		styleID = paragraph.Props.Style.Val
	}
	for _, style := range sheet.style_Chain(styleID) {
		paraState.apply(style.Paragraph)
		baseRun.apply(style.Run)
	}
	paraState.apply(paragraph.Props)
	info := paragraphInfo{
		InTable:     inTable,
		Align:       paraState.align,
		LineSpacing: paraState.lineSpacing,
		FirstLineMM: paraState.firstLineMM,
		HasIndent:   paraState.hasIndent,
		LeftMM:      paraState.leftMM,
		Numbered:    paraState.numbered,
	}
	runs := append([]xRun{}, paragraph.Runs...)
	for _, link := range paragraph.Hyperlinks {
		runs = append(runs, link.Runs...)
	}
	for _, field := range paragraph.Fields {
		runs = append(runs, field.Runs...)
	}
	var text strings.Builder
	for _, run := range runs {
		state := baseRun
		if run.Props != nil && run.Props.Style != nil {
			for _, style := range sheet.style_Chain(run.Props.Style.Val) {
				state.apply(style.Run)
			}
		}
		state.apply(run.Props)
		runText := strings.Join(run.Texts, "")
		if len(run.Tabs) > 0 {
			runText += " "
		}
		text.WriteString(runText)
		font := state.font
		switch {
		case font != "":
		case strings.HasPrefix(state.theme, "major"):
			font = sheet.majorFont
		case state.theme != "":
			font = sheet.minorFont
		}
		info.Runs = append(info.Runs, runInfo{Text: runText, Font: font, SizePt: state.size, Color: state.color})
	}
	info.Text = strings.TrimSpace(text.String())
	return info
}

func twips_ToMM(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return value / twipsPerMM
}

func check_OnOff(value *xOnOff) bool {
	if value == nil {
		return false
	}
	return value.Val == nil || check_True(*value.Val)
}

func check_True(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on":
		return true
	}
	return false
}

func first_NonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
