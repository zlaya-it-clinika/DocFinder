package httpapi

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"docfinder/core/internal/blank"
	"docfinder/core/internal/layout"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

// Режимы чата. «Чтение» - ответы по базе знаний, документы не начинаются.
// «План» - бот готовит документ: продолжает начатый, узнает вид по словам, затем с помощью модели,
// а если вид так и не понятен - предлагает список того, что умеет готовить

// Список предложен, ждем номер или название. Request - исходная просьба: из нее макет берет сведения
type planChoice struct {
	Request string `json:"request"`
}

// Документ, который бот умеет готовить: макет (internal/layout) или бланк модуля (internal/blank)
type planDocument struct {
	ID     string
	Name   string
	Layout *texts.LayoutInfo
}

type planResult struct {
	Layout *layout.Step
	Blank  *blank.Step
	// Список документов на выбор или отмена выбора
	Reply *assistantReply
	// Ответы на поля документа пачкой - для записи реплики в историю
	Answered []answerLine
}

// Режим из сообщения, иначе сохраненный в сессии. false - неизвестный режим в сообщении
func resolve_ChatMode(requested *string, saved string) (string, bool) {
	if requested != nil {
		mode := strings.TrimSpace(*requested)
		if mode != texts.ChatModeRead && mode != texts.ChatModePlan {
			return "", false
		}
		return mode, true
	}
	if saved == texts.ChatModePlan {
		return texts.ChatModePlan, true
	}
	return texts.ChatModeDefault, true
}

// Режим для истории сессии: сохраненный или режим по умолчанию
func saved_ChatMode(saved string) string {
	mode, _ := resolve_ChatMode(nil, saved)
	return mode
}

// В «Чтении» начатый документ и предложенный список забываются: следующая реплика - вопрос к базе знаний
func clear_DocumentStates(session *sessionRecord) {
	session.blankFill = blank.FillState{}
	session.layoutFill = layout.State{}
	session.planChoice = nil
	session.templateStep = templateStepIdle
}

func refresh_TemplateStep(session *sessionRecord) {
	session.templateStep = templateStepIdle
	if session.blankFill.BlankID != "" || session.layoutFill.LayoutID != "" || session.planChoice != nil {
		session.templateStep = templateStepAsked
	}
}

// Ответ «Чтения» на прямую просьбу подготовить документ получает подсказку про режим «План»
func apply_ModeHint(reply assistantReply, mode, text string) assistantReply {
	if mode != texts.ChatModeRead || !layout.Check_IsRequest(text) {
		return reply
	}
	if _, found := layout.Match_Document(text); found {
		reply.Hint = texts.ChatModePlanHint
		return reply
	}
	if _, found := blank.Match_Blank(text); found {
		reply.Hint = texts.ChatModePlanHint
	}
	return reply
}

// Ход «Плана». answers - ответы на поля начатого документа пачкой (карточка вопросов),
// без них реплика отвечает на текущий вопрос. loadOrganization вызывается, только когда готовится макет
func advance_PlanTurn(session *sessionRecord, text string, answers []fieldAnswer, loadOrganization func() (layout.Organization, error), format layout.Format, chatter layout.Chatter, now time.Time) (planResult, error) {
	defer refresh_TemplateStep(session)
	if session.blankFill.BlankID != "" {
		if len(answers) == 0 && layout.Check_IsCancel(text) {
			session.blankFill = blank.FillState{}
			return cancelled_PlanResult(), nil
		}
		blankID := session.blankFill.BlankID
		var step blank.Step
		var handled bool
		if len(answers) > 0 {
			step, handled = blank.Answer_Fields(session.blankFill, blank_Answers(answers))
		} else {
			step, handled = blank.Advance_Blank(session.blankFill, text)
		}
		if handled {
			session.blankFill = step.State
			answered := answer_Lines(answers, func(field string) string { return blank.Field_Label(blankID, field) })
			return planResult{Blank: &step, Answered: answered}, nil
		}
		session.blankFill = blank.FillState{}
	}
	if session.layoutFill.LayoutID != "" {
		organization, err := loadOrganization()
		if err != nil {
			return planResult{}, err
		}
		layoutID := session.layoutFill.LayoutID
		var step layout.Step
		if len(answers) > 0 {
			step = layout.Answer_Fields(session.layoutFill, layout_Answers(answers), organization, format, chatter, now)
		} else {
			step = layout.Advance_Layout(session.layoutFill, text, organization, format, chatter, now)
		}
		session.layoutFill = step.State
		answered := answer_Lines(answers, func(field string) string { return layout.Field_Label(layoutID, field) })
		return planResult{Layout: &step, Answered: answered}, nil
	}
	documents := list_PlanDocuments()
	request := text
	lead := texts.PlanChoiceText
	if session.planChoice != nil {
		request = session.planChoice.Request
		session.planChoice = nil
		if layout.Check_IsCancel(text) {
			return cancelled_PlanResult(), nil
		}
		if document, found := pick_PlanDocument(documents, text); found {
			return start_PlanDocument(session, document, request, loadOrganization, format, chatter, now)
		}
		// Вместо номера - новая просьба: разбираем ее как первую, а если и она непонятна - список еще раз
		request = text
		lead = texts.PlanChoiceRetryText
	}
	document, found := match_PlanDocument(documents, request)
	if !found {
		document, found = classify_PlanDocument(chatter, documents, request)
	}
	if !found {
		session.planChoice = &planChoice{Request: request}
		reply := build_PlanChoiceReply(documents, lead)
		return planResult{Reply: &reply}, nil
	}
	return start_PlanDocument(session, document, request, loadOrganization, format, chatter, now)
}

func cancelled_PlanResult() planResult {
	reply := build_PlainReply(texts.KindAnswer, texts.LayoutCancelledText)
	return planResult{Reply: &reply}
}

// Макеты, затем бланки модулей. Порядок задает номера в списке на выбор
func list_PlanDocuments() []planDocument {
	documents := []planDocument{}
	for _, info := range texts.Layout_Catalog() {
		info := info
		documents = append(documents, planDocument{ID: info.ID, Name: info.Name, Layout: &info})
	}
	if blanks, err := blank.Catalog(); err == nil {
		for _, item := range blanks {
			documents = append(documents, planDocument{ID: item.ID, Name: item.Name})
		}
	}
	return documents
}

// Вид документа по словам реплики: сначала макеты, затем бланки модулей
func match_PlanDocument(documents []planDocument, text string) (planDocument, bool) {
	if info, found := layout.Match_Document(text); found {
		return find_PlanDocument(documents, info.ID)
	}
	if item, found := blank.Match_Blank(text); found {
		return find_PlanDocument(documents, item.ID)
	}
	return planDocument{}, false
}

// Ответ на список: номер («3», «3.»), название целиком (кнопка варианта) или слова названия
func pick_PlanDocument(documents []planDocument, text string) (planDocument, bool) {
	if number, err := strconv.Atoi(strings.Trim(text, " .)")); err == nil {
		if number >= 1 && number <= len(documents) {
			return documents[number-1], true
		}
		return planDocument{}, false
	}
	for _, document := range documents {
		if strings.EqualFold(strings.TrimSpace(text), document.Name) {
			return document, true
		}
	}
	return match_PlanDocument(documents, text)
}

func find_PlanDocument(documents []planDocument, id string) (planDocument, bool) {
	for _, document := range documents {
		if document.ID == id {
			return document, true
		}
	}
	return planDocument{}, false
}

// Модель выбирает вид документа по смыслу просьбы. Ответ - ровно один код из списка, иначе модель не уверена
func classify_PlanDocument(chatter layout.Chatter, documents []planDocument, request string) (planDocument, bool) {
	if chatter == nil {
		return planDocument{}, false
	}
	var list strings.Builder
	for _, document := range documents {
		fmt.Fprintf(&list, "%s - %s\n", document.ID, document.Name)
	}
	raw, err := chatter.Complete_Chat([]ollama.ChatMessage{
		{Role: "system", Content: texts.PlanClassifyPrompt},
		{Role: "user", Content: texts.PlanDocumentsLabel + ":\n" + list.String() + "\n" + texts.LayoutRequestLabel + ": " + request},
	})
	if err != nil {
		return planDocument{}, false
	}
	// Коды сравниваются целыми словами: «act» не должен находиться внутри «contract»
	tokens := strings.FieldsFunc(strings.ToLower(raw), func(symbol rune) bool {
		return !unicode.IsLetter(symbol) && !unicode.IsDigit(symbol) && symbol != '-'
	})
	var chosen []planDocument
	for _, document := range documents {
		for _, token := range tokens {
			if token == document.ID {
				chosen = append(chosen, document)
				break
			}
		}
	}
	if len(chosen) != 1 {
		return planDocument{}, false
	}
	return chosen[0], true
}

func start_PlanDocument(session *sessionRecord, document planDocument, request string, loadOrganization func() (layout.Organization, error), format layout.Format, chatter layout.Chatter, now time.Time) (planResult, error) {
	if document.Layout != nil {
		organization, err := loadOrganization()
		if err != nil {
			return planResult{}, err
		}
		step := layout.Start_Layout(*document.Layout, request, organization, format, chatter, now)
		session.layoutFill = step.State
		return planResult{Layout: &step}, nil
	}
	step, found := blank.Start_Blank(document.ID)
	if !found {
		session.planChoice = &planChoice{Request: request}
		reply := build_PlanChoiceReply(list_PlanDocuments(), texts.PlanChoiceRetryText)
		return planResult{Reply: &reply}, nil
	}
	session.blankFill = step.State
	return planResult{Blank: &step}, nil
}

// Текст со списком - для клиентов без карточки вопросов, options - готовые ответы кнопками
func build_PlanChoiceReply(documents []planDocument, lead string) assistantReply {
	lines := []string{lead}
	options := make([]string, 0, len(documents))
	for index, document := range documents {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, document.Name))
		options = append(options, document.Name)
	}
	reply := build_PlainReply(texts.KindTemplateChoice, strings.Join(lines, "\n"))
	reply.Options = options
	return reply
}
