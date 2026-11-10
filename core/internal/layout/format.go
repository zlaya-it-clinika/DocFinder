package layout

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Правила оформления макетов не зашиты в код: они лежат в профилях formats/*.json.
// instruction.json - Инструкция по ДОУ Организации, gost.json - ГОСТ Р 7.0.97-2025,
// base.json - значения шаблона там, где документы правила не задают (отступы между блоками, размер герба).
// У каждого правила - пункт документа, откуда оно взято.
// Правило берется из первого профиля, где оно есть. Порядок - по полю priority профилей:
// Инструкция важнее ГОСТ, ГОСТ закрывает то, о чем Инструкция молчит.
// LAYOUT_FORMAT_PRIORITY в .env переставляет профили: перечисленные идут первыми, остальные - за ними по priority

//go:embed formats/*.json
var formatFiles embed.FS

// Значения перечислимых правил в профилях
const (
	PageNumberHeader       = "header"
	PageNumberFooter       = "footer"
	NameOrderInitialsFirst = "initials-surname"
	NameOrderSurnameFirst  = "surname-initials"
	// Отметка об исполнителе как ее написал сотрудник
	NameOrderAsIs        = "as-is"
	CommitteeLabelGroup  = "group"
	CommitteeLabelEach   = "each"
	dateTokenDay         = "DD"
	dateTokenMonth       = "MM"
	dateTokenYear        = "YYYY"
	goDateLayoutDay      = "02"
	goDateLayoutMonth    = "01"
	goDateLayoutFullYear = "2006"
)

var paragraphAligns = map[string]bool{"left": true, "center": true, "right": true, "both": true}

// Итоговые правила оформления. Ключ json - имя правила в профиле
type Format struct {
	PageWidthMM             float64 `json:"pageWidthMM"`
	PageHeightMM            float64 `json:"pageHeightMM"`
	MarginLeftMM            float64 `json:"marginLeftMM"`
	MarginRightMM           float64 `json:"marginRightMM"`
	MarginTopMM             float64 `json:"marginTopMM"`
	MarginBottomMM          float64 `json:"marginBottomMM"`
	FontName                string  `json:"fontName"`
	FontSizePt              float64 `json:"fontSizePt"`
	SmallFontSizePt         float64 `json:"smallFontSizePt"`
	TextLineSpacing         float64 `json:"textLineSpacing"`
	RequisiteLineSpacing    float64 `json:"requisiteLineSpacing"`
	FirstLineIndentMM       float64 `json:"firstLineIndentMM"`
	TextAlign               string  `json:"textAlign"`
	AddresseeZoneMM         float64 `json:"addresseeZoneMM"`
	PageNumberPart          string  `json:"pageNumberPart"`
	PageNumberAlign         string  `json:"pageNumberAlign"`
	PageNumberOnFirstPage   bool    `json:"pageNumberOnFirstPage"`
	HeaderFooterDistanceMM  float64 `json:"headerFooterDistanceMM"`
	InitialsSeparator       string  `json:"initialsSeparator"`
	HeaderNameOrder         string  `json:"headerNameOrder"`
	SignatureNameOrder      string  `json:"signatureNameOrder"`
	ExecutorNameOrder       string  `json:"executorNameOrder"`
	ExecutorAtPageBottom    bool    `json:"executorAtPageBottom"`
	CommitteeMemberLabel    string  `json:"committeeMemberLabel"`
	DateFormat              string  `json:"dateFormat"`
	EmblemHeightMM          float64 `json:"emblemHeightMM"`
	StampMaxWidthMM         float64 `json:"stampMaxWidthMM"`
	StampMaxHeightMM        float64 `json:"stampMaxHeightMM"`
	LetterAddresseeIndentMM float64 `json:"letterAddresseeIndentMM"`
	LetterDateSpaceMM       float64 `json:"letterDateSpaceMM"`
	TypeTitleSpaceMM        float64 `json:"typeTitleSpaceMM"`
	DateLineSpaceMM         float64 `json:"dateLineSpaceMM"`
	TitleSpaceMM            float64 `json:"titleSpaceMM"`
	GreetingSpaceMM         float64 `json:"greetingSpaceMM"`
	FirstSignatureSpaceMM   float64 `json:"firstSignatureSpaceMM"`
	SignatureGapMM          float64 `json:"signatureGapMM"`
	ExecutorSpaceMM         float64 `json:"executorSpaceMM"`
	// Откуда взято каждое правило: ключ правила - профиль и пункт. Нужно проверке оформления
	Refs map[string]FormatRef `json:"-"`
}

// Источник правила: название профиля («Инструкция по ДОУ Организации») и пункт с пояснением
type FormatRef struct {
	Profile string
	Text    string
}

type formatRule struct {
	Value json.RawMessage `json:"value"`
	Ref   string          `json:"ref"`
}

type formatProfile struct {
	Name     string                `json:"-"`
	Title    string                `json:"title"`
	Priority int                   `json:"priority"`
	Rules    map[string]formatRule `json:"rules"`
}

// Правила оформления по профилям. order - имена профилей из LAYOUT_FORMAT_PRIORITY, пустой - порядок по priority.
// Ошибка, если правило без ссылки на пункт, неизвестное правило или какого-то правила нет ни в одном профиле
func Load_Format(order []string) (Format, error) {
	profiles, err := read_FormatProfiles()
	if err != nil {
		return Format{}, err
	}
	ordered, err := order_FormatProfiles(profiles, order)
	if err != nil {
		return Format{}, err
	}
	merged := map[string]json.RawMessage{}
	refs := map[string]FormatRef{}
	for _, profile := range ordered {
		for key, rule := range profile.Rules {
			if strings.TrimSpace(rule.Ref) == "" {
				return Format{}, fmt.Errorf("layout format: rule %q in %s has no ref", key, profile.Name)
			}
			if _, taken := merged[key]; !taken {
				merged[key] = rule.Value
				refs[key] = FormatRef{Profile: profile.Title, Text: rule.Ref}
			}
		}
	}
	if missing := find_MissingRules(merged); len(missing) > 0 {
		return Format{}, fmt.Errorf("layout format: no profile sets %s", strings.Join(missing, ", "))
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return Format{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var format Format
	if err := decoder.Decode(&format); err != nil {
		return Format{}, fmt.Errorf("layout format: %w", err)
	}
	format.Refs = refs
	return format, format.validate()
}

func read_FormatProfiles() (map[string]formatProfile, error) {
	entries, err := formatFiles.ReadDir("formats")
	if err != nil {
		return nil, err
	}
	profiles := map[string]formatProfile{}
	for _, entry := range entries {
		raw, err := formatFiles.ReadFile(path.Join("formats", entry.Name()))
		if err != nil {
			return nil, err
		}
		var profile formatProfile
		if err := json.Unmarshal(raw, &profile); err != nil {
			return nil, fmt.Errorf("layout format %s: %w", entry.Name(), err)
		}
		profile.Name = strings.TrimSuffix(entry.Name(), path.Ext(entry.Name()))
		profiles[profile.Name] = profile
	}
	return profiles, nil
}

// Сначала профили из order, затем остальные по priority (при равенстве - по имени)
func order_FormatProfiles(profiles map[string]formatProfile, order []string) ([]formatProfile, error) {
	result := []formatProfile{}
	used := map[string]bool{}
	for _, name := range order {
		name = strings.TrimSpace(name)
		if name == "" || used[name] {
			continue
		}
		profile, found := profiles[name]
		if !found {
			return nil, fmt.Errorf("layout format: unknown profile %q in LAYOUT_FORMAT_PRIORITY", name)
		}
		used[name] = true
		result = append(result, profile)
	}
	rest := []formatProfile{}
	for name, profile := range profiles {
		if !used[name] {
			rest = append(rest, profile)
		}
	}
	sort.Slice(rest, func(left, right int) bool {
		if rest[left].Priority == rest[right].Priority {
			return rest[left].Name < rest[right].Name
		}
		return rest[left].Priority < rest[right].Priority
	})
	return append(result, rest...), nil
}

func find_MissingRules(merged map[string]json.RawMessage) []string {
	missing := []string{}
	formatType := reflect.TypeOf(Format{})
	for index := 0; index < formatType.NumField(); index++ {
		key := strings.Split(formatType.Field(index).Tag.Get("json"), ",")[0]
		if key == "-" {
			continue
		}
		if _, found := merged[key]; !found {
			missing = append(missing, key)
		}
	}
	return missing
}

func (format Format) validate() error {
	sizes := map[string]float64{
		"pageWidthMM": format.PageWidthMM, "pageHeightMM": format.PageHeightMM,
		"fontSizePt": format.FontSizePt, "smallFontSizePt": format.SmallFontSizePt,
		"textLineSpacing": format.TextLineSpacing, "requisiteLineSpacing": format.RequisiteLineSpacing,
	}
	for key, value := range sizes {
		if value <= 0 {
			return fmt.Errorf("layout format: %s must be positive", key)
		}
	}
	if format.text_WidthMM() <= format.AddresseeZoneMM {
		return fmt.Errorf("layout format: margins leave no room for the addressee zone")
	}
	if strings.TrimSpace(format.FontName) == "" {
		return fmt.Errorf("layout format: fontName is empty")
	}
	if !paragraphAligns[format.TextAlign] || !paragraphAligns[format.PageNumberAlign] {
		return fmt.Errorf("layout format: unknown align %q or %q", format.TextAlign, format.PageNumberAlign)
	}
	if format.PageNumberPart != PageNumberHeader && format.PageNumberPart != PageNumberFooter {
		return fmt.Errorf("layout format: unknown pageNumberPart %q", format.PageNumberPart)
	}
	for key, value := range map[string]string{"headerNameOrder": format.HeaderNameOrder, "signatureNameOrder": format.SignatureNameOrder} {
		if value != NameOrderInitialsFirst && value != NameOrderSurnameFirst {
			return fmt.Errorf("layout format: unknown %s %q", key, value)
		}
	}
	if format.ExecutorNameOrder != NameOrderAsIs && format.ExecutorNameOrder != NameOrderInitialsFirst && format.ExecutorNameOrder != NameOrderSurnameFirst {
		return fmt.Errorf("layout format: unknown executorNameOrder %q", format.ExecutorNameOrder)
	}
	if format.CommitteeMemberLabel != CommitteeLabelGroup && format.CommitteeMemberLabel != CommitteeLabelEach {
		return fmt.Errorf("layout format: unknown committeeMemberLabel %q", format.CommitteeMemberLabel)
	}
	for _, token := range []string{dateTokenDay, dateTokenMonth, dateTokenYear} {
		if !strings.Contains(format.DateFormat, token) {
			return fmt.Errorf("layout format: dateFormat %q has no %s", format.DateFormat, token)
		}
	}
	return nil
}

func (format Format) text_WidthMM() float64 {
	return format.PageWidthMM - format.MarginLeftMM - format.MarginRightMM
}

// «DD.MM.YYYY» -> «15.08.2026»
func (format Format) format_Date(moment time.Time) string {
	layout := strings.NewReplacer(dateTokenYear, goDateLayoutFullYear, dateTokenMonth, goDateLayoutMonth, dateTokenDay, goDateLayoutDay).Replace(format.DateFormat)
	return moment.Format(layout)
}

// Буквы инициалов -> «М.С.» или «М. С.» по правилу initialsSeparator
func (format Format) initials_Text(letters []string) string {
	parts := make([]string, 0, len(letters))
	for _, letter := range letters {
		parts = append(parts, letter+".")
	}
	return strings.Join(parts, format.InitialsSeparator)
}

// Фамилия с инициалами в нужном порядке. Без фамилии - пусто
func (format Format) person_Name(parsed person, order string) string {
	if parsed.Surname == "" {
		return ""
	}
	initials := format.initials_Text(parsed.Initials)
	if initials == "" {
		return parsed.Surname
	}
	if order == NameOrderSurnameFirst {
		return parsed.Surname + " " + initials
	}
	return initials + " " + parsed.Surname
}

// Подпись: «И. И. Иванов» по правилу signatureNameOrder
func (format Format) signature_Name(parsed person) string {
	return format.person_Name(parsed, format.SignatureNameOrder)
}

// Адресат и составитель в шапке: «И. И. Иванову» или «Иванову И.И.» по правилу headerNameOrder
func (format Format) header_Name(parsed person) string {
	return format.person_Name(parsed, format.HeaderNameOrder)
}

// Отметка об исполнителе: «Петрова Мария Сергеевна, +7 ...» -> «М. С. Петрова, +7 ...».
// Если ФИО не разобралось или правило as-is, строка остается как ее написал сотрудник
func (format Format) executor_Text(text string) string {
	text = strings.TrimSpace(text)
	if format.ExecutorNameOrder == NameOrderAsIs || text == "" {
		return text
	}
	namePart, contacts, _ := strings.Cut(text, ",")
	parsed := parse_Person(namePart)
	if parsed.Position != "" || len(parsed.Initials) == 0 {
		return text
	}
	name := format.person_Name(parsed, format.ExecutorNameOrder)
	if contacts = strings.TrimSpace(contacts); contacts != "" {
		return name + ", " + contacts
	}
	return name
}
