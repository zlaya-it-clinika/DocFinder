package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"docfinder/core/internal/blank"
	"docfinder/core/internal/layout"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/texts"
)

// Макеты документов в чате: служебная записка, письмо, акт и другие. Оформление - по Инструкции по ДОУ,
// о чем она молчит - по ГОСТ Р 7.0.97-2025 (профили internal/layout/formats).
// Реквизиты организации (должностные лица, герб, штамп) - из вкладки «Организация»

// Состояние шаблона в сессии: бланк модуля, макет и список документов на выбор (режим «План»).
// Ключи layout и choice не мешают старым сессиям с одним бланком
type sessionTemplateState struct {
	blank.FillState
	Layout *layout.State `json:"layout,omitempty"`
	Choice *planChoice   `json:"choice,omitempty"`
}

func encode_SessionState(session *sessionRecord) []byte {
	if session.layoutFill.LayoutID == "" && session.planChoice == nil {
		return encode_FillState(session.blankFill)
	}
	state := sessionTemplateState{FillState: session.blankFill, Choice: session.planChoice}
	if session.layoutFill.LayoutID != "" {
		state.Layout = &session.layoutFill
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func decode_TemplateState(raw []byte) sessionTemplateState {
	var state sessionTemplateState
	if len(raw) > 0 && json.Unmarshal(raw, &state) != nil {
		return sessionTemplateState{}
	}
	return state
}

func decode_LayoutState(raw []byte) layout.State {
	if state := decode_TemplateState(raw); state.Layout != nil {
		return *state.Layout
	}
	return layout.State{}
}

func decode_PlanChoice(raw []byte) *planChoice {
	return decode_TemplateState(raw).Choice
}

func reply_FromLayout(step layout.Step) assistantReply {
	if step.Cancelled {
		return build_PlainReply(texts.KindAnswer, texts.LayoutCancelledText)
	}
	if step.Ready {
		reply := build_PlainReply(texts.KindTemplateReady, strings.ReplaceAll(texts.LayoutReadyText, "{name}", step.Name))
		reply.Template = &templateState{ID: step.LayoutID, Title: step.Name, DraftText: step.DraftText}
		return reply
	}
	reply := build_PlainReply(texts.KindTemplateField, step.Question)
	reply.Template = &templateState{ID: step.LayoutID, Title: step.Name, Field: step.Field, FieldLabel: step.FieldLabel, Questions: questions_FromLayout(step.Questions)}
	return reply
}

// Реквизиты организации для макета из боевого хранилища. Нет картинки - документ без нее
func (api *liveAPI) load_LayoutOrganization(ctx context.Context, token string) (layout.Organization, error) {
	organization, err := api.store.Read_Organization(ctx, token)
	if err != nil {
		return layout.Organization{}, err
	}
	result := layout.Organization{Officials: layout_Officials(organization.Officials)}
	for _, kind := range []string{imageKindEmblem, imageKindStamp} {
		image, imageErr := api.store.Read_OrganizationImage(ctx, token, kind)
		if errors.Is(imageErr, livestore.ErrNotFound) {
			continue
		}
		if imageErr != nil {
			return layout.Organization{}, imageErr
		}
		picture := &layout.Image{ContentType: image.ContentType, Body: image.Body}
		if kind == imageKindEmblem {
			result.Emblem = picture
		} else {
			result.Stamp = picture
		}
	}
	return result, nil
}

func layout_Officials(officials []livestore.Official) []layout.Official {
	result := make([]layout.Official, 0, len(officials))
	for _, official := range officials {
		result = append(result, layout.Official{Position: official.Position, FullName: official.FullName, Department: official.Department, Chief: official.Chief})
	}
	return result
}

// Реквизиты заглушки без проверки прав: вызывается из обработчика чата, который уже проверил сотрудника
func (organization *stubOrganization) layout_Snapshot() layout.Organization {
	if organization == nil {
		return layout.Organization{}
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	result := layout.Organization{Officials: layout_Officials(organization.sorted_Officials())}
	if image, found := organization.images[imageKindEmblem]; found {
		result.Emblem = &layout.Image{ContentType: image.ContentType, Body: image.Body}
	}
	if image, found := organization.images[imageKindStamp]; found {
		result.Stamp = &layout.Image{ContentType: image.ContentType, Body: image.Body}
	}
	return result
}
