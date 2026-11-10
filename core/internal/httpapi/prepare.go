package httpapi

import (
	"net/http"
	"strings"

	"docfinder/core/internal/draftcheck"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/texts"
)

func instruction_Source(lines []livestore.DocumentLine) draftcheck.Source {
	source := draftcheck.Source{
		Title:   texts.SeedInstructionTitle,
		Edition: texts.EditionDefault,
		Scope:   texts.ScopeGlobal,
	}
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line.Title), "инструкц") {
			source.DocumentID = line.DocumentID
			source.Title = line.Title
			source.Edition = line.Edition
			source.Scope = line.Scope
			break
		}
	}
	return source
}

func shown_FromCitations(items []citation) []shownDocument {
	documents := make([]shownDocument, 0, len(items))
	for _, item := range items {
		if item.DocumentID == "" {
			continue
		}
		documents = append(documents, shownDocument{
			DocumentID: item.DocumentID,
			Title:      item.Document,
			Paragraph:  item.Paragraph,
			Scope:      item.Scope,
		})
	}
	return documents
}

func write_FileResponse(responseWriter http.ResponseWriter, fileName, contentType string, body []byte) {
	responseWriter.Header().Set("Content-Type", contentType)
	responseWriter.Header().Set("Content-Disposition", build_AttachmentHeader(fileName))
	// Файл только скачивается: браузер не угадывает тип и не выполняет в нем скрипты
	responseWriter.Header().Set("X-Content-Type-Options", "nosniff")
	responseWriter.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	responseWriter.WriteHeader(http.StatusOK)
	_, _ = responseWriter.Write(body)
}

func blank_FileLink(blankID string) string {
	return "/blanks/" + blankID + "/file"
}

func draft_FileLink(draftID string) string {
	return "/drafts/" + draftID + "/file"
}
