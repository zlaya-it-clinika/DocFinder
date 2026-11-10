package livestore

import (
	"testing"

	"docfinder/core/internal/texts"
)

func chunk_ForReply(documentID string, distance float64) SimilarChunk {
	return SimilarChunk{
		DocumentID: documentID,
		Title:      documentID,
		Edition:    "1.0",
		Paragraph:  "3.2",
		Content:    "Текст фрагмента",
		Scope:      texts.ScopeGlobal,
		Distance:   distance,
	}
}

func TestChoose_LiveReplyKind(t *testing.T) {
	kind, selected := Choose_LiveReplyKind([]SimilarChunk{chunk_ForReply("doc-a", 0.7)}, nil)
	if kind != texts.KindFallback || selected != nil {
		t.Fatalf("weak %+v %s", selected, kind)
	}

	kind, selected = Choose_LiveReplyKind(nil, []SimilarChunk{chunk_ForReply("doc-closed", 0.2)})
	if kind != texts.KindAccessDenied || selected != nil {
		t.Fatalf("closed only %+v %s", selected, kind)
	}

	kind, selected = Choose_LiveReplyKind([]SimilarChunk{chunk_ForReply("doc-a", 0.7)}, []SimilarChunk{chunk_ForReply("doc-closed", 0.2)})
	if kind != texts.KindAccessDenied || selected != nil {
		t.Fatalf("closed beats weak %+v %s", selected, kind)
	}

	kind, selected = Choose_LiveReplyKind([]SimilarChunk{chunk_ForReply("doc-a", 0.2)}, []SimilarChunk{chunk_ForReply("doc-closed", 0.1)})
	if kind != texts.KindAnswer || len(selected) != 1 || selected[0].DocumentID != "doc-a" {
		t.Fatalf("visible wins %+v %s", selected, kind)
	}

	kind, selected = Choose_LiveReplyKind([]SimilarChunk{chunk_ForReply("doc-a", 0.2)}, nil)
	if kind != texts.KindAnswer || len(selected) != 1 {
		t.Fatalf("single %+v %s", selected, kind)
	}
	if Relevance_Percent(selected[0]) != 80 {
		t.Fatalf("percent %d", Relevance_Percent(selected[0]))
	}

	kind, selected = Choose_LiveReplyKind([]SimilarChunk{
		chunk_ForReply("doc-a", 0.2),
		chunk_ForReply("doc-b", 0.23),
	}, nil)
	if kind != texts.KindClarify || len(selected) != 2 {
		t.Fatalf("clarify %+v %s", selected, kind)
	}

	kind, selected = Choose_LiveReplyKind([]SimilarChunk{
		chunk_ForReply("doc-a", 0.2),
		chunk_ForReply("doc-a", 0.22),
	}, nil)
	if kind != texts.KindAnswer || len(selected) != 2 {
		t.Fatalf("same document %+v %s", selected, kind)
	}

	kind, selected = Choose_LiveReplyKind([]SimilarChunk{
		chunk_ForReply("doc-a", 0.2),
		chunk_ForReply("doc-b", 0.3),
	}, nil)
	if kind != texts.KindAnswer || len(selected) != 2 {
		t.Fatalf("wide gap %+v %s", selected, kind)
	}

	citations := Build_ReplyCitations(selected[:1])
	if len(citations) != 1 || citations[0].OriginalLink != "/documents/doc-a/file#p3.2" || citations[0].Quote != "Текст фрагмента" {
		t.Fatalf("citation %+v", citations)
	}
	if citations[0].DocumentID != "doc-a" || citations[0].Scope != texts.ScopeGlobal {
		t.Fatalf("citation scope %+v", citations[0])
	}
}

func TestChunk_EmbedTextAddsAddress(t *testing.T) {
	got := chunk_EmbedText(" Инструкция по ДОУ ", "6", "6.7", "", "Срочные документы.")
	if got != "Инструкция по ДОУ. "+texts.LiveSectionLabel+" 6. "+texts.LiveParagraphLabel+" 6.7.\nСрочные документы." {
		t.Fatalf("embed text %q", got)
	}
	if bare := chunk_EmbedText("Памятка", "", "", "", "Текст."); bare != "Памятка.\nТекст." {
		t.Fatalf("bare %q", bare)
	}
}
