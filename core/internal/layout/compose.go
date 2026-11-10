package layout

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"docfinder/core/internal/grounding"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

// Заголовок из самой просьбы без модели: «служебку о замене ноутбука» -> «о замене ноутбука»
var subjectPhrase = regexp.MustCompile(`(?i)(?:^|\s)(об?\s+\S.*)$`)

// Суть документа из первой просьбы принимается, только если в ней есть подробности
const minPointsWords = 8

// Запрос на закупку: «служебку на закупку ноутбуков» -> «о закупке ноутбуков»
var purchasePhrase = regexp.MustCompile(`(?i)(?:закупк\S*|приобретени\S*)\s+(\S.*)$`)

type parts struct {
	Addressee []string `json:"addressee"`
	Author    []string `json:"author"`
	Title     string   `json:"title"`
	Greeting  string   `json:"greeting"`
	Text      []string `json:"text"`
}

// Сведения, прямо названные в первой просьбе. Значение модели принимается, только если оно есть в просьбе
func extract_Answers(info texts.LayoutInfo, request string, chatter Chatter) map[string]string {
	answers := map[string]string{}
	if match := subjectPhrase.FindStringSubmatch(strings.TrimSpace(request)); match != nil {
		if _, ok := find_Field(info, texts.LayoutFieldSubject); ok && info.Kind != texts.LayoutKindProtocol {
			answers[texts.LayoutFieldSubject] = strings.Trim(match[1], " .!?")
		}
	}
	if _, found := answers[texts.LayoutFieldSubject]; !found && info.ID == "purchase-request" {
		if match := purchasePhrase.FindStringSubmatch(strings.TrimSpace(request)); match != nil {
			answers[texts.LayoutFieldSubject] = texts.LayoutPurchaseSubject + " " + strings.Trim(match[1], " .!?")
		}
	}
	if chatter == nil {
		return answers
	}
	var fields strings.Builder
	for _, field := range info.Fields {
		fmt.Fprintf(&fields, "%s - %s\n", field.Key, field.Label)
	}
	raw, err := chatter.Complete_Chat([]ollama.ChatMessage{
		{Role: "system", Content: texts.LayoutExtractPrompt},
		{Role: "user", Content: texts.LayoutFieldsLabel + ":\n" + fields.String() + "\n" + texts.LayoutRequestLabel + ": " + request},
	})
	if err != nil {
		return answers
	}
	var extracted map[string]any
	if json.Unmarshal([]byte(cut_JSONObject(raw)), &extracted) != nil {
		return answers
	}
	for _, field := range info.Fields {
		value, ok := extracted[field.Key].(string)
		value = strings.TrimSpace(value)
		if !ok || value == "" || !check_ValueFromRequest(value, request) {
			continue
		}
		// Суть из короткой просьбы («закупка ноутбуков») - не ответ на главный вопрос: его бот задаст сам
		if field.Key == texts.LayoutFieldPoints && len(split_Words(value)) < minPointsWords {
			continue
		}
		// Тема - только с предлогом «о/об», иначе модель возвращает всю просьбу целиком
		if field.Key == texts.LayoutFieldSubject && info.Kind != texts.LayoutKindProtocol && !check_HasAboutPrefix(value) {
			continue
		}
		answers[field.Key] = value
	}
	return answers
}

// Хотя бы одно значимое слово значения есть в просьбе, а все его числа - в просьбе
func check_ValueFromRequest(value, request string) bool {
	if len(grounding.Find_UnsupportedNumbers(value, []string{request})) > 0 {
		return false
	}
	requestWords := split_Words(request)
	for _, word := range split_Words(value) {
		if len([]rune(word)) < 4 {
			continue
		}
		// «Сидоров» в значении и «Сидорову» в просьбе: сравнение по основе без окончания
		stem := stem_Surname(word)
		for _, requestWord := range requestWords {
			if strings.HasPrefix(requestWord, stem) {
				return true
			}
		}
	}
	return false
}

// Подсказка модели с примерами ФИО, записанными по тем же правилам, что и документ
func build_ComposePrompt(format Format) string {
	sample := func(text string) string {
		return format.header_Name(parse_Person(text))
	}
	return strings.NewReplacer(
		"{sampleLetter}", sample(texts.LayoutSampleLetterAddressee),
		"{sampleInternal}", sample(texts.LayoutSampleInternalAddressee),
		"{sampleAuthor}", sample(texts.LayoutSampleAuthor),
	).Replace(texts.LayoutComposePrompt)
}

// Реквизиты и текст от модели. Число не из сведений или неверный ответ - сборка из ответов как есть
func compose_Parts(info texts.LayoutInfo, state State, chatter Chatter, date string, format Format) parts {
	fallback := fallback_Parts(info, state, format)
	if chatter == nil {
		return fallback
	}
	var answers strings.Builder
	sources := []string{state.Request, date}
	for _, field := range info.Fields {
		value := state.Answers[field.Key]
		if value == "" {
			continue
		}
		sources = append(sources, value)
		fmt.Fprintf(&answers, "- %s: %s\n", field.Label, value)
	}
	user := fmt.Sprintf("%s: %s %s\n%s: %s\n%s: %s\n%s:\n%s%s: %s",
		texts.LayoutKindLabel, info.Name, info.Title,
		texts.LayoutRuleLabel, info.TextRule,
		texts.LayoutDateLabel, date,
		texts.LayoutAnswersLabel, answers.String(),
		texts.LayoutRequestLabel, state.Request)
	raw, err := chatter.Complete_Chat([]ollama.ChatMessage{
		{Role: "system", Content: build_ComposePrompt(format)},
		{Role: "user", Content: user},
	})
	if err != nil {
		return fallback
	}
	var composed parts
	if json.Unmarshal([]byte(cut_JSONObject(raw)), &composed) != nil {
		return fallback
	}
	composed.Title = strings.Trim(strings.TrimSpace(composed.Title), ".")
	composed.Text = strip_LetterFrame(clean_Lines(composed.Text), composed.Greeting)
	composed.Addressee = normalize_HeaderLines(format, clean_Lines(composed.Addressee), info.Kind == texts.LayoutKindLetter)
	composed.Author = normalize_HeaderLines(format, clean_Lines(composed.Author), false)
	if len(composed.Text) == 0 {
		return fallback
	}
	// Заголовок к тексту отвечает на вопрос «о чем?» (п. 8.15 Инструкции, п. 5.17 ГОСТ). Тему сотрудника с предлогом берем дословно:
	// модель ее иногда искажает («Об акт о списании»). Своя формулировка модели - только когда темы нет
	if fallback.Title != "" && (check_HasAboutPrefix(fallback.Title) || info.Kind == texts.LayoutKindProtocol || !check_HasAboutPrefix(composed.Title)) {
		composed.Title = fallback.Title
	}
	composed.Title = fix_Preposition(composed.Title)
	if composed.Title == "" {
		return fallback
	}
	everything := strings.Join(append(append(append([]string{composed.Title, composed.Greeting}, composed.Addressee...), composed.Author...), composed.Text...), "\n")
	if len(grounding.Find_UnsupportedNumbers(everything, sources)) > 0 {
		return fallback
	}
	// Шапку, которую модель не вернула, берем из ответов
	if len(composed.Addressee) == 0 {
		composed.Addressee = fallback.Addressee
	}
	if info.Kind == texts.LayoutKindNote && len(composed.Author) == 0 {
		composed.Author = fallback.Author
	}
	if info.Kind != texts.LayoutKindNote {
		composed.Author = nil
	}
	if info.Kind != texts.LayoutKindLetter {
		composed.Greeting = ""
	}
	return composed
}

// Без модели падеж не поменять, но ФИО в шапке записывается по правилам оформления
func fallback_Parts(info texts.LayoutInfo, state State, format Format) parts {
	result := parts{Title: fix_Preposition(subject_Title(info, state.Answers[texts.LayoutFieldSubject]))}
	if addressee := state.Answers[texts.LayoutFieldAddressee]; addressee != "" {
		result.Addressee = normalize_HeaderLines(format, []string{addressee}, info.Kind == texts.LayoutKindLetter)
	}
	if author := state.Answers[texts.LayoutFieldAuthor]; author != "" && info.Kind == texts.LayoutKindNote {
		result.Author = normalize_HeaderLines(format, []string{texts.LayoutAuthorPrefix + " " + author}, false)
	}
	result.Text = clean_Lines(strings.Split(state.Answers[texts.LayoutFieldPoints], "\n"))
	return result
}

// «о замене ноутбука» -> «О замене ноутбука», «замена ноутбука» -> «О замена ноутбука» не делаем: без предлога - как есть
func subject_Title(info texts.LayoutInfo, subject string) string {
	subject = strings.Trim(strings.TrimSpace(subject), ".")
	if subject == "" || info.Kind == texts.LayoutKindProtocol {
		return subject
	}
	return capitalize_First(subject)
}

// Гласные, перед которыми предлог «об»: «об утверждении», но «о замене», «о единой»
const aboutVowels = "аиоуыэАИОУЫЭ"

func check_HasAboutPrefix(title string) bool {
	lower := strings.ToLower(title)
	return strings.HasPrefix(lower, "о ") || strings.HasPrefix(lower, "об ")
}

// «Об замене» -> «О замене», «о утверждении» -> «Об утверждении»
func fix_Preposition(title string) string {
	if !check_HasAboutPrefix(title) {
		return title
	}
	_, rest, _ := strings.Cut(title, " ")
	rest = strings.TrimSpace(rest)
	first, _ := utf8.DecodeRuneInString(rest)
	if strings.ContainsRune(aboutVowels, first) {
		return "Об " + rest
	}
	return "О " + rest
}

// Строки адресата и составителя: организация первой (в письме), должность и ФИО на разных строках,
// ФИО - инициалы и фамилия по правилам оформления (п. 8.12.1 Инструкции, п. 5.15 ГОСТ):
// «Генеральному директору Сидорову Петру Петровичу» -> «Генеральному директору», «П. П. Сидорову»
func normalize_HeaderLines(format Format, lines []string, letter bool) []string {
	organizations := []string{}
	rest := []string{}
	for _, line := range lines {
		if letter && check_IsOrganization(line) {
			organizations = append(organizations, line)
			continue
		}
		parsed := parse_Person(line)
		if parsed.Surname == "" || len(strings.Fields(parsed.FullName)) < 2 {
			rest = append(rest, line)
			continue
		}
		if parsed.PositionRaw != "" {
			rest = append(rest, parsed.PositionRaw)
		}
		rest = append(rest, format.header_Name(parsed))
	}
	return append(organizations, rest...)
}

var organizationForms = []string{"АО ", "ООО ", "ПАО ", "ЗАО ", "НАО ", "ФГУП ", "ФГБУ ", "ГУП ", "МУП ", "Министерств", "Федеральн", "Банк "}

func check_IsOrganization(line string) bool {
	if strings.ContainsAny(line, "\"«") {
		return true
	}
	for _, form := range organizationForms {
		if strings.HasPrefix(line, form) {
			return true
		}
	}
	return false
}

// Обращение и «С уважением,» ставит шаблон письма, в тексте модели они лишние
func strip_LetterFrame(lines []string, greeting string) []string {
	result := []string{}
	for _, line := range lines {
		if greeting != "" && strings.TrimSpace(line) == strings.TrimSpace(greeting) {
			continue
		}
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), texts.LayoutClosingPhrase))
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func clean_Lines(lines []string) []string {
	cleaned := []string{}
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return cleaned
}

func cut_JSONObject(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	return raw[start : end+1]
}

// Документ по виду бланка: шапка, преамбула, подписи
func build_Document(info texts.LayoutInfo, state State, composed parts, organization Organization, date string, format Format) document {
	answers := state.Answers
	doc := document{
		Kind:      info.Kind,
		TypeTitle: info.Title,
		DateLine:  strings.ReplaceAll(texts.LayoutDateNumberLine, "{date}", date),
		Title:     composed.Title,
		Text:      composed.Text,
		Attach:    answers[texts.LayoutFieldAttach],
		Place:     answers[texts.LayoutFieldPlace],
	}
	switch info.Kind {
	case texts.LayoutKindLetter:
		doc.Emblem, doc.Stamp = organization.Emblem, organization.Stamp
		doc.HeaderRight = composed.Addressee
		doc.Greeting = composed.Greeting
		doc.Closing = texts.LayoutClosingPhrase
		doc.Signatures = []signature{sign_Person(format, answers[texts.LayoutFieldSigner], "")}
		doc.Executor = format.executor_Text(answers[texts.LayoutFieldExecutor])
	case texts.LayoutKindAct:
		doc.Emblem, doc.Stamp = organization.Emblem, organization.Stamp
		if basis := answers[texts.LayoutFieldBasis]; basis != "" {
			doc.Preamble = append(doc.Preamble, texts.LayoutBasisLabel+" "+basis)
		}
		doc.Preamble = append(doc.Preamble, texts.LayoutCommitteeLabel+" "+answers[texts.LayoutFieldCommittee])
		doc.Signatures = committee_Signatures(format, answers[texts.LayoutFieldCommittee])
	case texts.LayoutKindProtocol:
		doc.Emblem, doc.Stamp = organization.Emblem, organization.Stamp
		doc.Preamble = []string{
			texts.LayoutChairLabel + " " + answers[texts.LayoutFieldChair],
			texts.LayoutSecretaryLabel + " " + answers[texts.LayoutFieldSecretary],
			texts.LayoutAttendeesLabel + " " + answers[texts.LayoutFieldAttendees],
			texts.LayoutAgendaLabel,
		}
		for index, item := range split_List(answers[texts.LayoutFieldAgenda]) {
			doc.Preamble = append(doc.Preamble, fmt.Sprintf("%d. %s", index+1, item))
		}
		doc.Signatures = []signature{
			sign_Person(format, answers[texts.LayoutFieldChair], texts.LayoutChairSign),
			sign_Person(format, answers[texts.LayoutFieldSecretary], texts.LayoutSecretarySign),
		}
	default:
		doc.HeaderRight = append(append([]string{}, composed.Addressee...), "")
		doc.HeaderRight = append(doc.HeaderRight, composed.Author...)
		doc.Signatures = []signature{sign_Person(format, answers[texts.LayoutFieldAuthor], "")}
	}
	return doc
}

// Подпись: должность слева, инициалы и фамилия справа (п. 8.19.2 Инструкции, п. 5.22 ГОСТ).
// status заменяет должность: «Председатель»
func sign_Person(format Format, text, status string) signature {
	parsed := parse_Person(text)
	position := parsed.Position
	if status != "" {
		position = status
	}
	name := format.signature_Name(parsed)
	if name == "" {
		name = strings.TrimSpace(text)
		if status == "" {
			position = ""
		}
	}
	return signature{Position: position, Name: name}
}

// Члены комиссии через «;»: председатель - строка со словом «председатель» или первая.
// Подпись члена комиссии - по правилу committeeMemberLabel: «Член комиссии» у каждого (п. 8.19.2 Инструкции)
// или «Члены комиссии» один раз над подписями (п. 5.22 ГОСТ)
func committee_Signatures(format Format, committee string) []signature {
	items := split_List(committee)
	if len(items) == 0 {
		return nil
	}
	chair := 0
	for index, item := range items {
		if strings.Contains(strings.ToLower(item), "председател") {
			chair = index
			break
		}
	}
	signatures := []signature{sign_Person(format, items[chair], texts.LayoutCommitteeChairSign)}
	first := true
	for index, item := range items {
		if index == chair {
			continue
		}
		status := " "
		switch {
		case format.CommitteeMemberLabel == CommitteeLabelEach:
			status = texts.LayoutCommitteeMemberSign
		case first:
			status = texts.LayoutCommitteeSign
		}
		first = false
		sign := sign_Person(format, item, status)
		sign.Position = strings.TrimSpace(sign.Position)
		signatures = append(signatures, sign)
	}
	return signatures
}

func split_List(text string) []string {
	return clean_Lines(strings.FieldsFunc(text, func(symbol rune) bool { return symbol == ';' || symbol == '\n' }))
}

// Текст документа для карточки в чате: те же реквизиты по порядку
func render_Plain(doc document) string {
	lines := []string{}
	add := func(values ...string) {
		lines = append(lines, values...)
	}
	switch doc.Kind {
	case texts.LayoutKindLetter:
		add(doc.DateLine, "")
		add(doc.HeaderRight...)
	case texts.LayoutKindAct, texts.LayoutKindProtocol:
		add(strings.ToUpper(doc.TypeTitle), doc.DateLine)
		if doc.Place != "" {
			add(doc.Place)
		}
	default:
		add(doc.HeaderRight...)
		add("", strings.ToUpper(doc.TypeTitle), doc.DateLine)
	}
	if doc.Title != "" {
		add("", doc.Title)
	}
	add("")
	add(doc.Preamble...)
	if doc.Greeting != "" {
		add(doc.Greeting, "")
	}
	add(doc.Text...)
	if doc.Attach != "" {
		add("", texts.LayoutAttachLabel+" "+doc.Attach)
	}
	if doc.Closing != "" {
		add("", doc.Closing)
	}
	add("")
	for _, sign := range doc.Signatures {
		add(strings.TrimSpace(sign.Position + "    " + sign.Name))
	}
	if doc.Executor != "" {
		add("", doc.Executor)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
