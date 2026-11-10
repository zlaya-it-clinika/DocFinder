package draftcheck

import (
	"strings"
	"testing"

	"docfinder/core/internal/texts"
)

func TestFind_ParagraphRequisites(t *testing.T) {
	for _, paragraph := range []string{texts.PrepareParagraphComposition, texts.PrepareParagraphList} {
		piece, found, err := Find_Paragraph(paragraph)
		if err != nil || !found || strings.TrimSpace(piece.Content) == "" {
			t.Fatalf("paragraph %s %+v found %v err %v", paragraph, piece, found, err)
		}
	}
}
