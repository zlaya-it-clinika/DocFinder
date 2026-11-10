package blank

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"html"
	"io"
	"regexp"
	"strings"
)

var (
	paragraphMarkup = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
	textRunMarkup   = regexp.MustCompile(`(?s)(<w:t(?:\s[^>]*)?>)(.*?)(</w:t>)`)
)

func Read_ModuleFile(id string) (string, []byte, error) {
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return "", nil, io.ErrUnexpectedEOF
	}
	name := id + ".docx"
	body, err := moduleFiles.ReadFile("modules/" + name)
	if err != nil {
		return "", nil, err
	}
	return name, body, nil
}

func Fill_ModuleDocx(id string, state FillState) ([]byte, error) {
	blanks, err := Catalog()
	if err != nil {
		return nil, err
	}
	item, found := find_Blank(blanks, id)
	if !found {
		return nil, io.ErrUnexpectedEOF
	}
	_, original, err := Read_ModuleFile(id)
	if err != nil {
		return nil, err
	}
	return fill_Docx(original, item, state)
}

func fill_Docx(original []byte, item Blank, state FillState) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(original), int64(len(original)))
	if err != nil {
		return nil, err
	}
	var rebuilt bytes.Buffer
	writer := zip.NewWriter(&rebuilt)
	for _, file := range reader.File {
		data, readErr := read_ZipFile(file)
		if readErr != nil {
			return nil, readErr
		}
		if file.Name == "word/document.xml" {
			data = fill_DocumentXML(data, item, state)
		}
		entry, createErr := writer.Create(file.Name)
		if createErr != nil {
			return nil, createErr
		}
		if _, writeErr := entry.Write(data); writeErr != nil {
			return nil, writeErr
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return rebuilt.Bytes(), nil
}

func read_ZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// Примечания и всё после них не входят в подписываемый файл.
// Текст абзаца собирается в первый фрагмент, оформление абзаца остаётся.
func fill_DocumentXML(raw []byte, item Blank, state FillState) []byte {
	xmlText := drop_Notes(string(raw))
	filled := paragraphMarkup.ReplaceAllStringFunc(xmlText, func(paragraph string) string {
		return fill_ParagraphXML(paragraph, item, state)
	})
	return []byte(filled)
}

func drop_Notes(xmlText string) string {
	for _, span := range paragraphMarkup.FindAllStringIndex(xmlText, -1) {
		paragraph := xmlText[span[0]:span[1]]
		if !strings.Contains(strings.ToLower(join_Runs(paragraph)), "примечани") {
			continue
		}
		tail := xmlText[span[0]:]
		section := strings.Index(tail, "<w:sectPr")
		if section < 0 {
			return xmlText[:span[0]]
		}
		return xmlText[:span[0]] + tail[section:]
	}
	return xmlText
}

func fill_ParagraphXML(paragraph string, item Blank, state FillState) string {
	runs := textRunMarkup.FindAllStringSubmatch(paragraph, -1)
	if len(runs) == 0 {
		return paragraph
	}
	var joined strings.Builder
	for _, run := range runs {
		joined.WriteString(html.UnescapeString(run[2]))
	}
	filled := escape_XMLText(clean_Draft(apply_Fields(joined.String(), item, state)))
	first := true
	return textRunMarkup.ReplaceAllStringFunc(paragraph, func(run string) string {
		parts := textRunMarkup.FindStringSubmatch(run)
		inner := ""
		if first {
			inner = filled
			first = false
		}
		return parts[1] + inner + parts[3]
	})
}

func join_Runs(paragraph string) string {
	var joined strings.Builder
	for _, run := range textRunMarkup.FindAllStringSubmatch(paragraph, -1) {
		joined.WriteString(html.UnescapeString(run[2]))
	}
	return joined.String()
}

func escape_XMLText(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

// Название из свойств документа (docProps/core.xml, dc:title): «Модуль распоряжения о создании рабочей группы».
// Заголовок в тексте модуля содержит пропуски и номера примечаний, для списков он не годится
func read_DocxTitle(body []byte) string {
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return ""
	}
	for _, file := range archive.File {
		if file.Name != "docProps/core.xml" {
			continue
		}
		raw, err := read_ZipFile(file)
		if err != nil {
			return ""
		}
		var properties struct {
			Title string `xml:"title"`
		}
		if xml.Unmarshal(raw, &properties) != nil {
			return ""
		}
		return strings.Join(strings.Fields(properties.Title), " ")
	}
	return ""
}
