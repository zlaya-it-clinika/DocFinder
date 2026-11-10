package httpapi

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/chunk"
	"docfinder/core/internal/draftassist"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

var errUnknownAttachment = errors.New("unknown attachment")

type revisionSource struct {
	attachmentID string
	title        string
	text         string
	files        []messageAttachment
}

func (api *liveAPI) serve_SessionAttachment(responseWriter http.ResponseWriter, request *http.Request) {
	fileName, body, _, err := read_UploadedFile(responseWriter, request)
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	plain, extractError := chunk.Extract_DocumentText(fileName, body)
	if extractError != nil || strings.TrimSpace(plain) == "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	saved, err := api.store.Create_SessionAttachment(request.Context(), token, request.PathValue("sessionId"), filepath.Base(fileName), strings.TrimSpace(plain))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, attachmentResponse{
		AttachmentID: saved.AttachmentID,
		FileName:     saved.FileName,
		Characters:   saved.Characters,
	})
}

func (api *liveAPI) revise_LiveDraft(ctx context.Context, account livestore.Account, sessionID, question, processID string, source livestore.RevisionSource) (assistantReply, []shownDocument, error) {
	embedding, err := api.store.Embed_Question(question)
	if err != nil {
		return assistantReply{}, nil, err
	}
	visible, _, err := api.store.Find_ReplyChunks(ctx, embedding, account.UserID, account.RoleIDs, processID)
	if err != nil {
		return assistantReply{}, nil, err
	}
	revised, changes, citations, err := build_DraftRevision(api.chatter, question, visible, source.Text)
	if err != nil {
		return assistantReply{}, nil, err
	}
	docxBody, err := draftassist.Build_Docx(revised)
	if err != nil {
		return assistantReply{}, nil, err
	}
	draftID, err := api.store.Save_SessionDraftFile(ctx, account.UserID, sessionID, docx_FileName(source.Title), docxBody)
	if err != nil {
		return assistantReply{}, nil, err
	}
	percent := 0
	if len(visible) > 0 {
		percent = livestore.Relevance_Percent(visible[0])
	}
	reply := build_PlainReply(texts.KindDraftRevision, draft_Summary(changes))
	reply.RelevancePercent = percent
	reply.Citations = citations
	reply.Draft = &draftState{
		AttachmentID: source.AttachmentID,
		Title:        source.Title,
		DraftText:    revised,
		Changes:      changes,
		DraftLink:    draft_FileLink(draftID),
	}
	return reply, shown_FromCitations(reply.Citations), nil
}

func (stubStore *stub) serve_SessionAttachment(responseWriter http.ResponseWriter, request *http.Request) {
	fileName, body, _, err := read_UploadedFile(responseWriter, request)
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	plain, extractError := chunk.Extract_DocumentText(fileName, body)
	if extractError != nil || strings.TrimSpace(plain) == "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	plain = strings.TrimSpace(plain)
	fileName = filepath.Base(fileName)
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	session, found := stubStore.find_OwnedSession(responseWriter, request)
	if !found {
		return
	}
	record := stubStore.usersByID[session.ownerID]
	if !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionDraftsAssist) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	if session.attachmentsByID == nil {
		session.attachmentsByID = map[string]*attachmentRecord{}
	}
	attachmentID := "att-" + strconv.Itoa(stubStore.nextAttachment)
	stubStore.nextAttachment++
	session.attachmentsByID[attachmentID] = &attachmentRecord{
		id:         attachmentID,
		fileName:   fileName,
		plainText:  plain,
		characters: utf8.RuneCountInString(plain),
	}
	write_JSONResponse(responseWriter, http.StatusOK, attachmentResponse{
		AttachmentID: attachmentID,
		FileName:     fileName,
		Characters:   utf8.RuneCountInString(plain),
	})
}

func (session *sessionRecord) revision_Source(rawIDs []string) (revisionSource, bool, error) {
	ids := clean_IDs(rawIDs)
	if len(ids) == 0 {
		if session.draftAttachmentID == "" || session.attachmentsByID[session.draftAttachmentID] == nil {
			return revisionSource{}, false, nil
		}
		attachment := session.attachmentsByID[session.draftAttachmentID]
		return revisionSource{
			attachmentID: attachment.id,
			title:        attachment.fileName,
			text:         attachment.plainText,
			files:        []messageAttachment{{AttachmentID: attachment.id, FileName: attachment.fileName}},
		}, true, nil
	}
	parts := make([]string, 0, len(ids))
	files := make([]messageAttachment, 0, len(ids))
	for _, attachmentID := range ids {
		attachment := session.attachmentsByID[attachmentID]
		if attachment == nil {
			return revisionSource{}, false, errUnknownAttachment
		}
		parts = append(parts, attachment.plainText)
		files = append(files, messageAttachment{AttachmentID: attachment.id, FileName: attachment.fileName})
	}
	return revisionSource{
		attachmentID: files[0].AttachmentID,
		title:        files[0].FileName,
		text:         strings.Join(parts, "\n\n"),
		files:        files,
	}, true, nil
}

// В заглушке нет модели, поэтому файл и сессия не меняются.
func (*stub) finish_StubRevision(_ *sessionRecord, source revisionSource) (assistantReply, []messageAttachment, error) {
	return build_PlainReply(texts.KindFallback, texts.FallbackText), source.files, nil
}

func build_DraftRevision(chatter interface {
	Complete_Chat(messages []ollama.ChatMessage) (string, error)
}, question string, chunks []livestore.SimilarChunk, draftText string) (string, []draftChange, []citation, error) {
	if chatter == nil {
		return "", nil, nil, livestore.ErrEmbedUnavailable
	}
	parts := draftassist.Split_DraftText(draftText)
	revisedParts := make([]string, 0, len(parts))
	var modelChanges []draftassist.PartChange
	for _, part := range parts {
		raw, err := chatter.Complete_Chat([]ollama.ChatMessage{
			{Role: "system", Content: texts.LiveDraftPrompt},
			{Role: "user", Content: format_DraftPart(question, chunks, part)},
		})
		if err != nil {
			return "", nil, nil, livestore.ErrEmbedUnavailable
		}
		parsed, parseError := draftassist.Parse_PartResult(raw)
		if parseError != nil {
			return "", nil, nil, livestore.ErrEmbedUnavailable
		}
		revisedParts = append(revisedParts, parsed.DraftText)
		modelChanges = append(modelChanges, parsed.Changes...)
	}
	changes, citations := map_DraftChanges(modelChanges, chunks)
	return draftassist.Join_DraftParts(revisedParts), changes, citations, nil
}

func format_DraftPart(question string, chunks []livestore.SimilarChunk, part string) string {
	var builder strings.Builder
	builder.WriteString(format_QuestionWithChunks(question, chunks))
	builder.WriteString("\n")
	builder.WriteString(texts.LiveDraftPartLabel)
	builder.WriteString(":\n")
	builder.WriteString(part)
	return builder.String()
}

func map_DraftChanges(modelChanges []draftassist.PartChange, chunks []livestore.SimilarChunk) ([]draftChange, []citation) {
	changes := make([]draftChange, 0, len(modelChanges))
	citations := []citation{}
	seen := map[string]struct{}{}
	for _, change := range modelChanges {
		text := strings.TrimSpace(change.Text)
		if text == "" {
			continue
		}
		item := draftChange{Text: text}
		if matched, ok := citation_ForParagraph(chunks, change.Paragraph); ok {
			matchedCopy := matched
			item.Citation = &matchedCopy
			key := matched.DocumentID + "\n" + matched.Paragraph
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				citations = append(citations, matched)
			}
		}
		changes = append(changes, item)
	}
	return changes, citations
}

func citation_ForParagraph(chunks []livestore.SimilarChunk, paragraph string) (citation, bool) {
	paragraph = strings.TrimSpace(paragraph)
	if paragraph == "" {
		return citation{}, false
	}
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.Paragraph) != paragraph {
			continue
		}
		built := livestore.Build_ReplyCitations([]livestore.SimilarChunk{chunk})
		if len(built) == 0 {
			return citation{}, false
		}
		item := built[0]
		return citation{
			Document:     item.Document,
			Edition:      item.Edition,
			Section:      item.Section,
			Paragraph:    item.Paragraph,
			Appendix:     item.Appendix,
			Quote:        item.Quote,
			OriginalLink: item.OriginalLink,
			DocumentID:   item.DocumentID,
			Scope:        item.Scope,
		}, true
	}
	return citation{}, false
}

func draft_Summary(changes []draftChange) string {
	lines := []string{}
	for _, change := range changes {
		text := strings.TrimSpace(change.Text)
		if text != "" {
			lines = append(lines, text)
		}
	}
	if len(lines) == 0 {
		return texts.DraftRevisionKept
	}
	return strings.Join(lines, "\n")
}

func message_Attachments(files []livestore.AttachmentFile) []messageAttachment {
	attachments := make([]messageAttachment, 0, len(files))
	for _, file := range files {
		attachments = append(attachments, messageAttachment{AttachmentID: file.AttachmentID, FileName: file.FileName})
	}
	return attachments
}

func docx_FileName(fileName string) string {
	base := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	base = strings.TrimSpace(base)
	if base == "" || base == "." {
		base = "draft"
	}
	return base + ".docx"
}

func clean_IDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleaned = append(cleaned, id)
		}
	}
	return cleaned
}
