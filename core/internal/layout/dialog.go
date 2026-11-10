package layout

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

// Диалог макета: наводящие вопросы по полям, ответы в состоянии сессии, в конце - проект .docx.
// Должностные лица, герб и штамп берутся из вкладки «Организация», правила оформления - из профилей (format.go)

type Official struct {
	Position   string
	FullName   string
	Department string
	Chief      bool
}

type Image struct {
	ContentType string
	Body        []byte
}

type Organization struct {
	Officials []Official
	Emblem    *Image
	Stamp     *Image
}

// Модель необязательна: без нее (заглушка, Ollama недоступна) документ собирается из ответов как есть
type Chatter interface {
	Complete_Chat(messages []ollama.ChatMessage) (string, error)
}

// Хранится в сессии рядом с состоянием бланка. Пустой LayoutID - макет не готовится
type State struct {
	LayoutID string            `json:"layoutId,omitempty"`
	Answers  map[string]string `json:"answers,omitempty"`
	Field    string            `json:"field,omitempty"`
	Request  string            `json:"request,omitempty"`
}

type Step struct {
	LayoutID   string
	Name       string
	Field      string
	FieldLabel string
	Question   string
	// Все поля без ответа, начиная с текущего: клиент задает их одной карточкой
	Questions []Question
	Ready     bool
	Cancelled bool
	DraftText string
	Docx      []byte
	FileName  string
	State     State
}

func Start_Layout(info texts.LayoutInfo, request string, organization Organization, format Format, chatter Chatter, now time.Time) Step {
	state := State{LayoutID: info.ID, Answers: map[string]string{}, Request: strings.TrimSpace(request)}
	for key, value := range extract_Answers(info, request, chatter) {
		// «Иванову» из просьбы - должностное лицо из вкладки «Организация», как и ответ на вопрос
		if key == texts.LayoutFieldAddressee || key == texts.LayoutFieldSigner {
			if official, ok := pick_Official(value, organization); ok {
				value = describe_Official(official)
			}
		}
		state.Answers[key] = value
	}
	step := next_Step(info, state, organization, format, chatter, now)
	if !step.Ready {
		step.Question = format_Name(texts.LayoutStartText, info.Name) + "\n\n" + step.Question
	}
	return step
}

func Advance_Layout(state State, text string, organization Organization, format Format, chatter Chatter, now time.Time) Step {
	info, found := Find_Layout(state.LayoutID)
	if !found {
		return Step{Cancelled: true}
	}
	if Check_IsCancel(text) {
		return Step{LayoutID: info.ID, Name: info.Name, Cancelled: true}
	}
	if state.Answers == nil {
		state.Answers = map[string]string{}
	}
	if field, ok := find_Field(info, state.Field); ok {
		answer, accepted := read_Answer(field, text, organization)
		if !accepted {
			return ask_Field(info, state, field, organization)
		}
		state.Answers[field.Key] = answer
	}
	return next_Step(info, state, organization, format, chatter, now)
}

func next_Step(info texts.LayoutInfo, state State, organization Organization, format Format, chatter Chatter, now time.Time) Step {
	for _, field := range info.Fields {
		if _, answered := state.Answers[field.Key]; answered {
			continue
		}
		return ask_Field(info, state, field, organization)
	}
	return finish_Layout(info, state, organization, format, chatter, now)
}

func ask_Field(info texts.LayoutInfo, state State, field texts.LayoutField, organization Organization) Step {
	state.Field = field.Key
	return Step{
		LayoutID:   info.ID,
		Name:       info.Name,
		Field:      field.Key,
		FieldLabel: field.Label,
		Question:   build_Question(field, organization),
		Questions:  build_Questions(info, state, organization),
		State:      state,
	}
}

// Вопрос поля. Адресату и подписанту - список должностных лиц из вкладки «Организация»
func build_Question(field texts.LayoutField, organization Organization) string {
	lines := []string{field.Question}
	if field.Key == texts.LayoutFieldAddressee || field.Key == texts.LayoutFieldSigner {
		if len(organization.Officials) == 0 {
			lines = append(lines, texts.LayoutOfficialsNone)
		} else {
			lines = append(lines, texts.LayoutOfficialsHint)
			for index, official := range organization.Officials {
				lines = append(lines, fmt.Sprintf("%d. %s", index+1, describe_Official(official)))
			}
		}
		if field.Key == texts.LayoutFieldSigner {
			if chief, ok := find_Chief(organization); ok {
				lines = append(lines, format_Name(texts.LayoutSignerDefault, describe_Official(chief)))
			}
		}
		return strings.Join(lines, "\n")
	}
	if field.Optional && !strings.Contains(field.Question, texts.BlankSkipNo+".") {
		lines = append(lines, texts.LayoutSkipHint)
	}
	return strings.Join(lines, "\n")
}

// Ответ на вопрос поля. false - ответ не подходит, вопрос повторяется
func read_Answer(field texts.LayoutField, text string, organization Organization) (string, bool) {
	answer := strings.TrimSpace(text)
	if field.Key == texts.LayoutFieldSigner && check_IsSkip(answer) {
		if chief, ok := find_Chief(organization); ok {
			return describe_Official(chief), true
		}
		return "", false
	}
	if field.Key == texts.LayoutFieldAddressee || field.Key == texts.LayoutFieldSigner {
		if official, ok := pick_Official(answer, organization); ok {
			return describe_Official(official), true
		}
	}
	if check_IsSkip(answer) {
		if field.Optional {
			return "", true
		}
		return "", false
	}
	return answer, true
}

// Номер из списка или фамилия должностного лица в ответе
func pick_Official(answer string, organization Organization) (Official, bool) {
	trimmed := strings.Trim(answer, " .)")
	if number, err := strconv.Atoi(trimmed); err == nil {
		if number >= 1 && number <= len(organization.Officials) {
			return organization.Officials[number-1], true
		}
		return Official{}, false
	}
	words := map[string]bool{}
	for _, word := range split_Words(answer) {
		words[word] = true
	}
	var found []Official
	for _, official := range organization.Officials {
		surname := strings.ToLower(parse_Person(official.FullName).Surname)
		if surname == "" {
			continue
		}
		// «Иванову» и «Иванов»: сравнение по основе фамилии без окончания падежа
		for word := range words {
			if strings.HasPrefix(word, stem_Surname(surname)) {
				found = append(found, official)
				break
			}
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return Official{}, false
}

func stem_Surname(surname string) string {
	runes := []rune(strings.ReplaceAll(surname, "ё", "е"))
	if len(runes) > 4 {
		runes = runes[:len(runes)-2]
	}
	return string(runes)
}

func describe_Official(official Official) string {
	parts := []string{official.Position}
	if official.Department != "" && !check_DepartmentInPosition(official.Position, official.Department) {
		parts = append(parts, official.Department)
	}
	return strings.Join(parts, ", ") + " " + official.FullName
}

// «Начальник отдела информационных технологий» уже называет «Отдел информационных технологий»:
// все значимые слова подразделения есть в должности с точностью до окончания
func check_DepartmentInPosition(position, department string) bool {
	positionWords := split_Words(position)
	for _, word := range split_Words(department) {
		if len([]rune(word)) < 3 {
			continue
		}
		stem := stem_Surname(word)
		found := false
		for _, positionWord := range positionWords {
			if strings.HasPrefix(positionWord, stem) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func find_Chief(organization Organization) (Official, bool) {
	for _, official := range organization.Officials {
		if official.Chief {
			return official, true
		}
	}
	return Official{}, false
}

func find_Field(info texts.LayoutInfo, key string) (texts.LayoutField, bool) {
	for _, field := range info.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return texts.LayoutField{}, false
}

func format_Name(template, name string) string {
	return strings.ReplaceAll(template, "{name}", name)
}

func finish_Layout(info texts.LayoutInfo, state State, organization Organization, format Format, chatter Chatter, now time.Time) Step {
	date := format.format_Date(now)
	parts := compose_Parts(info, state, chatter, date, format)
	doc := build_Document(info, state, parts, organization, date, format)
	body, err := build_Docx(doc, format)
	step := Step{
		LayoutID:  info.ID,
		Name:      info.Name,
		Ready:     true,
		DraftText: render_Plain(doc),
		FileName:  build_FileName(info, parts.Title),
		State:     State{},
	}
	if err == nil {
		step.Docx = body
	}
	return step
}

func build_FileName(info texts.LayoutInfo, title string) string {
	name := strings.TrimSpace(info.Name + " " + strings.ToLower(title))
	replacer := strings.NewReplacer("/", " ", "\\", " ", ":", " ", "*", " ", "?", " ", "\"", " ", "<", " ", ">", " ", "|", " ")
	name = strings.Join(strings.Fields(replacer.Replace(name)), " ")
	if runes := []rune(name); len(runes) > 80 {
		name = string(runes[:80])
	}
	if name == "" {
		name = texts.LayoutDefaultFileTitle
	}
	return name + texts.LayoutDocxSuffix
}
