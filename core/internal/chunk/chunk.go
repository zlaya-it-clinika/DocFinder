package chunk

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

const chunkRuneLimit = 800

var (
	sectionHeading   = regexp.MustCompile(`(?i)^\s*раздел\s+(\S+)`)
	paragraphHeading = regexp.MustCompile(`(?i)^\s*пункт\s+(\S+)`)
	appendixHeading  = regexp.MustCompile(`(?i)^\s*приложение\s+(\S+)`)
	numberedHeading  = regexp.MustCompile(`^\s*(\d+(?:\.\d+)+)\b`)
	// Обрывок номера пункта на отдельной строке PDF: «6.», «7», «6.1», «15»
	numberFragment = regexp.MustCompile(`^\d+(?:\.\d*)*$`)
)

var ErrUnsupportedFile = errors.New("unsupported file")

type TextChunk struct {
	Section   string
	Paragraph string
	Appendix  string
	Content   string
}

// TextExtractor читает текст одного формата. Новый формат регистрируется отдельно от загрузки.
type TextExtractor interface {
	Extract(fileName string, body []byte) (string, error)
}

type textExtractorFunc func(fileName string, body []byte) (string, error)

func (extract textExtractorFunc) Extract(fileName string, body []byte) (string, error) {
	return extract(fileName, body)
}

var textExtractors = map[string]TextExtractor{}

func init() {
	Register_TextExtractor(".txt", textExtractorFunc(func(fileName string, body []byte) (string, error) {
		return string(body), nil
	}))
	Register_TextExtractor(".pdf", textExtractorFunc(func(fileName string, body []byte) (string, error) {
		return extract_PdfText(body)
	}))
	Register_TextExtractor(".docx", textExtractorFunc(func(fileName string, body []byte) (string, error) {
		return extract_DocxText(body)
	}))
}

func Register_TextExtractor(extension string, extractor TextExtractor) {
	textExtractors[strings.ToLower(extension)] = extractor
}

func Supports_DocumentText(fileName string) bool {
	_, ok := textExtractors[strings.ToLower(path.Ext(fileName))]
	return ok
}

func Extract_DocumentText(fileName string, body []byte) (string, error) {
	extractor, ok := textExtractors[strings.ToLower(path.Ext(fileName))]
	if !ok {
		return "", ErrUnsupportedFile
	}
	return extractor.Extract(fileName, body)
}

func extract_PdfText(body []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	for pageNumber := 1; pageNumber <= reader.NumPage(); pageNumber++ {
		page := reader.Page(pageNumber)
		if page.V.IsNull() {
			continue
		}
		plain, plainError := page.GetPlainText(nil)
		if plainError != nil {
			return "", plainError
		}
		builder.WriteString(plain)
		builder.WriteByte('\n')
	}
	return join_PdfNumberLines(builder.String()), nil
}

// PDF отдает номер пункта кусками на разных строках: «6.1» / «5» / «. Снятие...» вместо «6.15. Снятие...».
// Без склейки пункт 6.15 попадает в 6.1, а 6.7 - в текст соседнего пункта.
// Склейка начинается только с обрывка с точкой, чтобы не трогать номера страниц
func join_PdfNumberLines(plain string) string {
	lines := strings.Split(plain, "\n")
	joined := make([]string, 0, len(lines))
	for index := 0; index < len(lines); index++ {
		first := strings.TrimSpace(lines[index])
		if !numberFragment.MatchString(first) || !strings.Contains(first, ".") {
			joined = append(joined, lines[index])
			continue
		}
		number := first
		next := index + 1
		for next < len(lines) {
			fragment := strings.TrimSpace(lines[next])
			if fragment != "" && !numberFragment.MatchString(fragment) {
				break
			}
			number += fragment
			next++
		}
		if next == len(lines) {
			joined = append(joined, number)
			index = next - 1
			continue
		}
		text := strings.TrimSpace(lines[next])
		if strings.HasPrefix(text, ".") {
			joined = append(joined, number+text)
		} else {
			joined = append(joined, number+" "+text)
		}
		index = next
	}
	return strings.Join(joined, "\n")
}

func extract_DocxText(body []byte) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", err
	}
	var documentXML *zip.File
	for _, file := range archive.File {
		if file.Name == "word/document.xml" {
			documentXML = file
			break
		}
	}
	if documentXML == nil {
		return "", errors.New("docx has no document.xml")
	}
	reader, err := documentXML.Open()
	if err != nil {
		return "", err
	}
	defer reader.Close()
	decoder := xml.NewDecoder(reader)
	var builder strings.Builder
	for {
		token, tokenError := decoder.Token()
		if tokenError == io.EOF {
			break
		}
		if tokenError != nil {
			return "", tokenError
		}
		switch node := token.(type) {
		case xml.CharData:
			builder.Write(node)
		case xml.EndElement:
			if node.Name.Local == "p" {
				builder.WriteByte('\n')
			}
		}
	}
	return builder.String(), nil
}

func Split_DocumentText(raw string) []TextChunk {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	var chunks []TextChunk
	var section, paragraph, appendix string
	var current strings.Builder
	structured := false
	flush := func() {
		text := strings.TrimSpace(current.String())
		current.Reset()
		if text == "" {
			return
		}
		chunks = append(chunks, TextChunk{
			Section:   section,
			Paragraph: paragraph,
			Appendix:  appendix,
			Content:   text,
		})
	}
	for _, line := range strings.Split(normalized, "\n") {
		kind, value, remainder, isHeading := classify_HeadingLine(line)
		if !isHeading {
			current.WriteString(line)
			current.WriteByte('\n')
			continue
		}
		structured = true
		flush()
		switch kind {
		case "section":
			section = value
			paragraph = ""
		case "appendix":
			appendix = value
			section = ""
			paragraph = ""
		default:
			paragraph = value
		}
		if remainder != "" {
			current.WriteString(remainder)
			current.WriteByte('\n')
		}
	}
	flush()
	if !structured {
		return split_FixedRunes(normalized, chunkRuneLimit)
	}
	return chunks
}

// Строка с разделом, пунктом или приложением задаёт метаданные фрагмента.
// Текст без такой структуры режется кусками фиксированной длины.
func classify_HeadingLine(line string) (kind, value, remainder string, ok bool) {
	trimmed := strings.TrimSpace(line)
	patterns := []struct {
		kind    string
		pattern *regexp.Regexp
	}{
		{kind: "section", pattern: sectionHeading},
		{kind: "paragraph", pattern: paragraphHeading},
		{kind: "appendix", pattern: appendixHeading},
		{kind: "paragraph", pattern: numberedHeading},
	}
	for _, candidate := range patterns {
		match := candidate.pattern.FindStringSubmatchIndex(trimmed)
		if match == nil {
			continue
		}
		value = strings.Trim(trimmed[match[2]:match[3]], ".:)")
		remainder = strings.TrimSpace(trimmed[match[1]:])
		return candidate.kind, value, remainder, true
	}
	return "", "", "", false
}

func split_FixedRunes(raw string, limit int) []TextChunk {
	runes := []rune(strings.TrimSpace(raw))
	if len(runes) == 0 {
		return nil
	}
	chunks := make([]TextChunk, 0, (len(runes)+limit-1)/limit)
	for start := 0; start < len(runes); start += limit {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		piece := strings.TrimSpace(string(runes[start:end]))
		if piece == "" {
			continue
		}
		chunks = append(chunks, TextChunk{Content: piece})
	}
	return chunks
}

func runeCount(text string) int {
	return utf8.RuneCountInString(text)
}
