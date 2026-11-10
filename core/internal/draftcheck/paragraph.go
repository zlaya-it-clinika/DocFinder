package draftcheck

import (
	"strings"

	"docfinder/core/internal/chunk"
)

func Find_Paragraph(paragraph string) (chunk.TextChunk, bool, error) {
	_, chunks, err := instruction_Text()
	if err != nil {
		return chunk.TextChunk{}, false, err
	}
	best, found := chunk_ForParagraph(chunks, paragraph)
	if found {
		return best, true, nil
	}
	needle := paragraph + "."
	for _, piece := range chunks {
		if strings.Contains(piece.Content, needle) {
			return piece, true, nil
		}
	}
	return chunk.TextChunk{}, false, nil
}

func chunk_ForParagraph(chunks []chunk.TextChunk, paragraph string) (chunk.TextChunk, bool) {
	var best chunk.TextChunk
	found := false
	for _, piece := range chunks {
		if piece.Paragraph != paragraph && !strings.HasPrefix(piece.Paragraph, paragraph+".") {
			continue
		}
		if !found || paragraph_Rank(piece, paragraph) < paragraph_Rank(best, paragraph) {
			best = piece
			found = true
		}
	}
	return best, found
}

func paragraph_Rank(piece chunk.TextChunk, paragraph string) int {
	if strings.Contains(piece.Content, paragraph+".") {
		return len(piece.Content)
	}
	return 1000000 + len(piece.Content)
}
