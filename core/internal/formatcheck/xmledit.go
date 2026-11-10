package formatcheck

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// Правка WordprocessingML без полного пересохранения: меняются только нужные элементы,
// остальной текст файла (пространства имен, расширения Word, рисунки) остается байт в байт.
// Префикс w: - стандартный для документов Word

type xmlSpan struct {
	Start, End int
	Name       string
	// Предки элемента по локальным именам, от корня
	Path []string
}

// Все элементы с позициями в исходном тексте. Фрагмент без объявлений пространств имен тоже читается:
// неизвестный префикс encoding/xml оставляет как есть
func scan_Elements(raw []byte) ([]xmlSpan, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = false
	type open struct {
		start int
		name  string
	}
	stack := []open{}
	spans := []xmlSpan{}
	for {
		before := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			stack = append(stack, open{start: before, name: element.Name.Local})
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unbalanced xml")
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			path := make([]string, 0, len(stack))
			for _, item := range stack {
				path = append(path, item.name)
			}
			spans = append(spans, xmlSpan{Start: top.start, End: int(decoder.InputOffset()), Name: top.name, Path: path})
		}
	}
	sort.Slice(spans, func(left, right int) bool { return spans[left].Start < spans[right].Start })
	return spans, nil
}

func (span xmlSpan) has_Ancestor(name string) bool {
	for _, item := range span.Path {
		if item == name {
			return true
		}
	}
	return false
}

// Прямые дочерние элементы: пути на один длиннее, чем у родителя, и внутри его границ
func direct_Children(spans []xmlSpan, parent xmlSpan) []xmlSpan {
	children := []xmlSpan{}
	for _, span := range spans {
		if span.Start > parent.Start && span.End <= parent.End && len(span.Path) == len(parent.Path)+1 {
			children = append(children, span)
		}
	}
	return children
}

type xmlEdit struct {
	Start, End int
	Text       string
}

// Правки не пересекаются; применяются с конца, чтобы позиции не сдвигались
func apply_Edits(raw []byte, edits []xmlEdit) []byte {
	sort.Slice(edits, func(left, right int) bool { return edits[left].Start > edits[right].Start })
	result := append([]byte{}, raw...)
	for _, edit := range edits {
		result = append(result[:edit.Start], append([]byte(edit.Text), result[edit.End:]...)...)
	}
	return result
}

// Порядок дочерних элементов по схеме OOXML: Word не открывает файл, если порядок нарушен
var (
	paragraphPropsOrder = []string{"pStyle", "keepNext", "keepLines", "pageBreakBefore", "framePr", "widowControl", "numPr", "suppressLineNumbers",
		"pBdr", "shd", "tabs", "suppressAutoHyphens", "kinsoku", "wordWrap", "overflowPunct", "topLinePunct", "autoSpaceDE", "autoSpaceDN", "bidi",
		"adjustRightInd", "snapToGrid", "spacing", "ind", "contextualSpacing", "mirrorIndents", "suppressOverlap", "jc", "textDirection",
		"textAlignment", "textboxTightWrap", "outlineLvl", "divId", "cnfStyle", "rPr", "sectPr", "pPrChange"}
	runPropsOrder = []string{"rStyle", "rFonts", "b", "bCs", "i", "iCs", "caps", "smallCaps", "strike", "dstrike", "outline", "shadow", "emboss",
		"imprint", "noProof", "snapToGrid", "vanish", "webHidden", "color", "spacing", "w", "kern", "position", "sz", "szCs", "highlight", "u",
		"effect", "bdr", "shd", "fitText", "vertAlign", "rtl", "cs", "em", "lang", "eastAsianLayout", "specVanish", "oMath"}
	sectionPropsOrder = []string{"headerReference", "footerReference", "footnotePr", "endnotePr", "type", "pgSz", "pgMar", "paperSrc", "pgBorders",
		"lnNumType", "pgNumType", "cols", "formProt", "vAlign", "noEndnote", "titlePg", "textDirection", "bidi", "rtlGutter", "docGrid",
		"printerSettings", "sectPrChange"}
)

type xmlChild struct {
	Name string
	Raw  string
}

// Свойства (pPr, rPr, sectPr) как список дочерних элементов
type xmlProps struct {
	Open     string
	Close    string
	Children []xmlChild
	order    []string
}

var openTag = regexp.MustCompile(`^<[^>]*?>`)

func parse_Props(raw string, order []string) (xmlProps, error) {
	open := openTag.FindString(raw)
	if open == "" {
		return xmlProps{}, fmt.Errorf("bad props")
	}
	props := xmlProps{order: order}
	name := strings.Fields(strings.Trim(open, "</>"))[0]
	if strings.HasSuffix(open, "/>") {
		props.Open = strings.TrimSuffix(open, "/>") + ">"
		props.Close = "</" + name + ">"
		return props, nil
	}
	props.Open = open
	props.Close = "</" + name + ">"
	inner := raw[len(open) : len(raw)-len(props.Close)]
	spans, err := scan_Elements([]byte("<x>" + inner + "</x>"))
	if err != nil {
		return xmlProps{}, err
	}
	for _, span := range spans {
		if len(span.Path) == 1 {
			props.Children = append(props.Children, xmlChild{Name: span.Name, Raw: inner[span.Start-3 : span.End-3]})
		}
	}
	return props, nil
}

func (props *xmlProps) find(name string) (int, bool) {
	for index, child := range props.Children {
		if child.Name == name {
			return index, true
		}
	}
	return -1, false
}

// Заменяет элемент или добавляет его на место по схеме
func (props *xmlProps) set(name, raw string) {
	if index, found := props.find(name); found {
		props.Children[index].Raw = raw
		return
	}
	props.Children = append(props.Children, xmlChild{Name: name, Raw: raw})
}

func (props *xmlProps) remove(name string) {
	kept := props.Children[:0]
	for _, child := range props.Children {
		if child.Name != name {
			kept = append(kept, child)
		}
	}
	props.Children = kept
}

func (props *xmlProps) get(name string) string {
	if index, found := props.find(name); found {
		return props.Children[index].Raw
	}
	return ""
}

func (props xmlProps) String() string {
	rank := map[string]int{}
	for index, name := range props.order {
		rank[name] = index
	}
	children := append([]xmlChild{}, props.Children...)
	sort.SliceStable(children, func(left, right int) bool {
		leftRank, leftKnown := rank[children[left].Name]
		rightRank, rightKnown := rank[children[right].Name]
		if !leftKnown {
			leftRank = len(props.order)
		}
		if !rightKnown {
			rightRank = len(props.order)
		}
		return leftRank < rightRank
	})
	var builder strings.Builder
	builder.WriteString(props.Open)
	for _, child := range children {
		builder.WriteString(child.Raw)
	}
	builder.WriteString(props.Close)
	return builder.String()
}

// Атрибуты w:name="value" в открывающем теге: заменить или добавить. Пустое значение удаляет атрибут
func set_Attributes(raw string, values map[string]string) string {
	open := openTag.FindString(raw)
	if open == "" {
		return raw
	}
	tag := open
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		pattern := regexp.MustCompile(`\s` + regexp.QuoteMeta(key) + `="[^"]*"`)
		switch {
		case value == "":
			tag = pattern.ReplaceAllString(tag, "")
		case pattern.MatchString(tag):
			tag = pattern.ReplaceAllString(tag, " "+key+`="`+value+`"`)
		default:
			end := len(tag) - 1
			if strings.HasSuffix(tag, "/>") {
				end = len(tag) - 2
			}
			tag = tag[:end] + " " + key + `="` + value + `"` + tag[end:]
		}
	}
	return tag + raw[len(open):]
}

func read_Attribute(raw, key string) string {
	match := regexp.MustCompile(`\s` + regexp.QuoteMeta(key) + `="([^"]*)"`).FindStringSubmatch(openTag.FindString(raw))
	if match == nil {
		return ""
	}
	return match[1]
}
