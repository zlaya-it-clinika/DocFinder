package chunk

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Таблица данных в .docx (например, Матрица визирования) режется по строкам: одна строка - один фрагмент,
// каждая ячейка подписана заголовком своего столбца. Без этого ячейки «Да», «Нет» идут подряд без названий
// столбцов, а кусок по 800 символов обрывает строку на середине.
// Маленькие таблицы (подпись, гриф, состав группы из пары строк) читаются как раньше, построчно

// Таблица считается таблицей данных: строка заголовков и не меньше стольких строк с данными
const minDataTableRows = 5

var rowNumber = regexp.MustCompile(`^(\d+)\.`)

type docxCell struct {
	text      string
	gridStart int
	gridSpan  int
}

type docxBlock struct {
	// Текст вне таблиц данных: абзацы и маленькие таблицы, по строке на абзац, как в Extract_DocumentText
	text string
	rows [][]docxCell
}

// Фрагменты документа для поиска. Для .docx таблицы данных режутся по строкам, остальное - как Split_DocumentText
func Extract_DocumentChunks(fileName string, body []byte) ([]TextChunk, error) {
	if strings.ToLower(path.Ext(fileName)) != ".docx" {
		plain, err := Extract_DocumentText(fileName, body)
		if err != nil {
			return nil, err
		}
		return Split_DocumentText(plain), nil
	}
	blocks, err := read_DocxBlocks(body)
	if err != nil {
		return nil, err
	}
	chunks := []TextChunk{}
	for _, block := range blocks {
		if block.rows == nil {
			chunks = append(chunks, Split_DocumentText(block.text)...)
			continue
		}
		chunks = append(chunks, split_TableRows(block.rows)...)
	}
	return chunks, nil
}

// Абзацы и таблицы верхнего уровня по порядку. Вложенная таблица - часть текста своей ячейки
func read_DocxBlocks(body []byte) ([]docxBlock, error) {
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	var documentXML *zip.File
	for _, file := range archive.File {
		if file.Name == "word/document.xml" {
			documentXML = file
			break
		}
	}
	if documentXML == nil {
		return nil, errors.New("docx has no document.xml")
	}
	reader, err := documentXML.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var blocks []docxBlock
	var text strings.Builder
	var paragraph strings.Builder
	tableDepth := 0
	var rows [][]docxCell
	var row []docxCell
	var cellParts []string
	// Абзацы таблицы построчно: если таблица окажется маленькой, она уйдет в текст как раньше
	var tableLines strings.Builder
	cellSpan, gridColumn := 1, 0
	decoder := xml.NewDecoder(reader)
	for {
		token, tokenError := decoder.Token()
		if tokenError == io.EOF {
			break
		}
		if tokenError != nil {
			return nil, tokenError
		}
		switch node := token.(type) {
		case xml.CharData:
			paragraph.Write(node)
		case xml.StartElement:
			switch node.Name.Local {
			case "tbl":
				tableDepth++
				if tableDepth == 1 {
					rows, tableLines = nil, strings.Builder{}
				}
			case "tr":
				if tableDepth == 1 {
					row, gridColumn = nil, 0
				}
			case "tc":
				if tableDepth == 1 {
					cellParts, cellSpan = nil, 1
				}
			case "gridSpan":
				if tableDepth == 1 {
					cellSpan = read_SpanValue(node)
				}
			}
		case xml.EndElement:
			switch node.Name.Local {
			case "p":
				line := paragraph.String()
				paragraph.Reset()
				if tableDepth == 0 {
					text.WriteString(line)
					text.WriteByte('\n')
					continue
				}
				tableLines.WriteString(line)
				tableLines.WriteByte('\n')
				if trimmed := strings.Join(strings.Fields(line), " "); trimmed != "" {
					cellParts = append(cellParts, trimmed)
				}
			case "tc":
				if tableDepth == 1 {
					// Абзацы ячейки - обычно отдельные пункты: «Департамент ИБ», «Департамент ИТ»
					row = append(row, docxCell{text: strings.Join(cellParts, "; "), gridStart: gridColumn, gridSpan: cellSpan})
					gridColumn += cellSpan
				}
			case "tr":
				if tableDepth == 1 {
					rows = append(rows, row)
				}
			case "tbl":
				tableDepth--
				if tableDepth > 0 {
					continue
				}
				if !check_IsDataTable(rows) {
					text.WriteString(tableLines.String())
					continue
				}
				blocks = append(blocks, docxBlock{text: text.String()}, docxBlock{rows: rows})
				text.Reset()
			}
		}
	}
	// Текст после последнего абзаца Extract_DocumentText тоже оставляет
	text.WriteString(paragraph.String())
	return append(blocks, docxBlock{text: text.String()}), nil
}

func read_SpanValue(node xml.StartElement) int {
	for _, attribute := range node.Attr {
		if attribute.Name.Local != "val" {
			continue
		}
		if span, err := strconv.Atoi(attribute.Value); err == nil && span > 0 {
			return span
		}
	}
	return 1
}

// Строка заголовков из нескольких столбцов и достаточно строк, где заполнено больше одной ячейки
func check_IsDataTable(rows [][]docxCell) bool {
	if len(rows) == 0 || len(rows[0]) < 2 {
		return false
	}
	dataRows := 0
	for _, row := range rows[1:] {
		if count_FilledCells(row) > 1 {
			dataRows++
		}
	}
	return dataRows >= minDataTableRows
}

func count_FilledCells(row []docxCell) int {
	filled := 0
	for _, cell := range row {
		if cell.text != "" {
			filled++
		}
	}
	return filled
}

// Строка с одной заполненной ячейкой (например, «ВНЕШНИЕ ДОКУМЕНТЫ») - раздел для следующих строк
func split_TableRows(rows [][]docxCell) []TextChunk {
	header := rows[0]
	chunks := []TextChunk{}
	section := ""
	for _, row := range rows[1:] {
		if count_FilledCells(row) == 0 {
			continue
		}
		if count_FilledCells(row) == 1 && len(row) == 1 {
			section = row[0].text
			continue
		}
		lines := []string{}
		if section != "" {
			lines = append(lines, section)
		}
		labels := []string{}
		values := map[string][]string{}
		for _, cell := range row {
			if cell.text == "" {
				continue
			}
			label := header_ForColumn(header, cell.gridStart)
			if _, seen := values[label]; !seen {
				labels = append(labels, label)
			}
			values[label] = append(values[label], cell.text)
		}
		// Одинаковые заголовки (объединенные столбцы) - одна строка со значениями через запятую
		for _, label := range labels {
			value := strings.Join(values[label], ", ")
			if label == "" {
				lines = append(lines, value)
				continue
			}
			lines = append(lines, label+": "+value)
		}
		paragraph := ""
		if match := rowNumber.FindStringSubmatch(row[0].text); match != nil {
			paragraph = match[1]
		}
		chunks = append(chunks, TextChunk{Section: section, Paragraph: paragraph, Content: strings.Join(lines, "\n")})
	}
	return chunks
}

// Заголовок столбца, который накрывает позицию ячейки в сетке таблицы
func header_ForColumn(header []docxCell, gridColumn int) string {
	for _, cell := range header {
		if gridColumn >= cell.gridStart && gridColumn < cell.gridStart+cell.gridSpan {
			return cell.text
		}
	}
	return ""
}
