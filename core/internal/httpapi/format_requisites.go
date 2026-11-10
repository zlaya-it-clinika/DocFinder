package httpapi

import (
	"net/http"
	"path/filepath"
	"strings"

	"docfinder/core/internal/formatcheck"
	"docfinder/core/internal/layout"
	"docfinder/core/internal/livestore"
)

// Реквизиты внешнего документа: чего нет (адресат, заголовок, подпись) и кого предложить из вкладки «Организация».
// Перед исправлением сотрудник отвечает, кто подписывает, кому документ и о чем он

type officialOption struct {
	OfficialID string `json:"officialId"`
	Label      string `json:"label"`
	Chief      bool   `json:"chief"`
}

type draftRequisites struct {
	// addressee, title, signature
	Missing   []string         `json:"missing"`
	Officials []officialOption `json:"officials"`
}

// nil - файл не .docx или все реквизиты на месте
func requisites_ForDraft(fileName string, body []byte, officials []livestore.Official) *draftRequisites {
	if !strings.EqualFold(filepath.Ext(fileName), ".docx") {
		return nil
	}
	missing, err := formatcheck.Find_MissingRequisites(body)
	if err != nil || len(missing) == 0 {
		return nil
	}
	options := make([]officialOption, 0, len(officials))
	for _, official := range officials {
		options = append(options, officialOption{
			OfficialID: official.OfficialID,
			Label:      strings.TrimSpace(official.Position + " " + official.FullName),
			Chief:      official.Chief,
		})
	}
	return &draftRequisites{Missing: missing, Officials: options}
}

// Ответы сотрудника из формы: signerId, addresseeId - должностные лица, title - о чем документ.
// Подпись и адресат записываются по правилам оформления, адресат - в дательном падеже, если есть модель
func read_FixOptions(request *http.Request, officials []livestore.Official, format layout.Format, chatter layout.Chatter) formatcheck.FixOptions {
	options := formatcheck.FixOptions{Title: layout.Title_Text(request.FormValue("title"))}
	byID := map[string]livestore.Official{}
	for _, official := range officials {
		byID[official.OfficialID] = official
	}
	if signer, found := byID[strings.TrimSpace(request.FormValue("signerId"))]; found {
		options.SignerPosition, options.SignerName = layout.Signature_Parts(format, layout_Official(signer))
	}
	if addressee, found := byID[strings.TrimSpace(request.FormValue("addresseeId"))]; found {
		options.Addressee = layout.Addressee_Lines(format, layout_Official(addressee), chatter)
	}
	return options
}

func layout_Official(official livestore.Official) layout.Official {
	return layout.Official{Position: official.Position, FullName: official.FullName, Department: official.Department, Chief: official.Chief}
}

// Должностные лица заглушки для формы исправления
func (organization *stubOrganization) officials_Snapshot() []livestore.Official {
	if organization == nil {
		return nil
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	return organization.sorted_Officials()
}
