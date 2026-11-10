package draftassist

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"html"
	"io"
	"strings"
	"unicode/utf8"
)

const PartRuneLimit = 6000

type PartChange struct {
	Text      string `json:"text"`
	Paragraph string `json:"paragraph"`
}

type PartResult struct {
	DraftText string
	Changes   []PartChange
}

// Абзацы собираются в части не длиннее окна модели. Абзац длиннее лимита режется по рунам.
func Split_DraftText(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if strings.TrimSpace(normalized) == "" {
		return []string{normalized}
	}
	var parts []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, current.String())
		current.Reset()
	}
	for _, paragraph := range strings.Split(normalized, "\n\n") {
		if strings.TrimSpace(paragraph) == "" {
			continue
		}
		for _, piece := range split_Runes(paragraph, PartRuneLimit) {
			if current.Len() == 0 {
				current.WriteString(piece)
				continue
			}
			if utf8.RuneCountInString(current.String())+2+utf8.RuneCountInString(piece) > PartRuneLimit {
				flush()
				current.WriteString(piece)
				continue
			}
			current.WriteString("\n\n")
			current.WriteString(piece)
		}
	}
	flush()
	if len(parts) == 0 {
		return []string{normalized}
	}
	return parts
}

func Join_DraftParts(parts []string) string {
	return strings.Join(parts, "\n\n")
}

func Parse_PartResult(raw string) (PartResult, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
		cleaned = strings.TrimPrefix(cleaned, "json")
		cleaned = strings.TrimPrefix(cleaned, "JSON")
		if end := strings.LastIndex(cleaned, "```"); end >= 0 {
			cleaned = cleaned[:end]
		}
		cleaned = strings.TrimSpace(cleaned)
	}
	var body struct {
		DraftText string       `json:"draftText"`
		Changes   []PartChange `json:"changes"`
	}
	if err := json.Unmarshal([]byte(cleaned), &body); err != nil {
		return PartResult{}, err
	}
	if strings.TrimSpace(body.DraftText) == "" {
		return PartResult{}, errors.New("empty draft")
	}
	if body.Changes == nil {
		body.Changes = []PartChange{}
	}
	return PartResult{DraftText: body.DraftText, Changes: body.Changes}, nil
}

func Build_Docx(text string) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", packageRelsXML},
		{"word/document.xml", document_XML(text)},
	}
	for _, file := range files {
		writer, err := archive.Create(file.name)
		if err != nil {
			return nil, err
		}
		if _, err = io.WriteString(writer, file.body); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func split_Runes(text string, limit int) []string {
	if limit < 1 || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	var parts []string
	var builder strings.Builder
	count := 0
	for _, symbol := range text {
		if count == limit {
			parts = append(parts, builder.String())
			builder.Reset()
			count = 0
		}
		builder.WriteRune(symbol)
		count++
	}
	if builder.Len() > 0 {
		parts = append(parts, builder.String())
	}
	return parts
}

func document_XML(text string) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	builder.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		builder.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		builder.WriteString(html.EscapeString(line))
		builder.WriteString(`</w:t></w:r></w:p>`)
	}
	builder.WriteString(`</w:body></w:document>`)
	return builder.String()
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

const packageRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`
