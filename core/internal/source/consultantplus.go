package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const consultantHTTPTimeout = 30 * time.Second

// Лицензионный HTTP-клиент Консультант Плюс. Поля настоящего API поставщика
// подставляются в этом файле, когда придёт спецификация. Сайт consultant.ru не вызываем.
type ConsultantPlus struct {
	client *http.Client
}

func New_ConsultantPlus(client *http.Client) ConsultantPlus {
	if client == nil {
		client = &http.Client{Timeout: consultantHTTPTimeout}
	}
	return ConsultantPlus{client: client}
}

func (ConsultantPlus) Kind() string {
	return KindConsultantPlus
}

func (connector ConsultantPlus) Verify_Access(ctx context.Context, config ConnectorConfig) error {
	if err := check_ConsultantConfig(config); err != nil {
		return err
	}
	request, err := connector.new_Request(ctx, http.MethodGet, config, "/v1/status", nil)
	if err != nil {
		return err
	}
	return connector.do_JSON(request, nil)
}

func (connector ConsultantPlus) List_Items(ctx context.Context, cursor string, config ConnectorConfig) ([]RemoteItem, string, error) {
	if err := check_ConsultantConfig(config); err != nil {
		return nil, "", err
	}
	query := url.Values{}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	if strings.TrimSpace(config.Bank) != "" {
		query.Set("bank", strings.TrimSpace(config.Bank))
	}
	if strings.TrimSpace(config.Rubric) != "" {
		query.Set("rubric", strings.TrimSpace(config.Rubric))
	}
	path := "/v1/documents"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	request, err := connector.new_Request(ctx, http.MethodGet, config, path, nil)
	if err != nil {
		return nil, "", err
	}
	var payload struct {
		Items []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Edition  string `json:"edition"`
			FileName string `json:"fileName"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	if err := connector.do_JSON(request, &payload); err != nil {
		return nil, "", err
	}
	items := make([]RemoteItem, 0, len(payload.Items))
	for _, item := range payload.Items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		fileName := item.FileName
		if fileName == "" {
			fileName = item.ID + ".txt"
		}
		items = append(items, RemoteItem{
			ID:       item.ID,
			Title:    item.Title,
			Edition:  item.Edition,
			FileName: fileName,
		})
	}
	return items, payload.NextCursor, nil
}

func (connector ConsultantPlus) Fetch_Item(ctx context.Context, item RemoteItem, config ConnectorConfig) (FetchedFile, error) {
	if err := check_ConsultantConfig(config); err != nil {
		return FetchedFile{}, err
	}
	if strings.TrimSpace(item.ID) == "" {
		return FetchedFile{}, ErrNotConfigured
	}
	path := "/v1/documents/" + url.PathEscape(item.ID) + "/file"
	request, err := connector.new_Request(ctx, http.MethodGet, config, path, nil)
	if err != nil {
		return FetchedFile{}, err
	}
	response, err := connector.client.Do(request)
	if err != nil {
		return FetchedFile{}, ErrSourceUnreachable
	}
	defer response.Body.Close()
	if err := map_ConsultantStatus(response.StatusCode); err != nil {
		return FetchedFile{}, err
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 20<<20))
	if err != nil {
		return FetchedFile{}, ErrSourceUnreachable
	}
	fileName := item.FileName
	if fileName == "" {
		fileName = item.ID + ".txt"
	}
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = contentType_ForName(fileName)
	}
	title := item.Title
	if title == "" {
		title = fileName
	}
	return FetchedFile{
		ExternalID:  item.ID,
		Title:       title,
		Edition:     item.Edition,
		FileName:    fileName,
		ContentType: contentType,
		Body:        body,
		Checksum:    Checksum_Bytes(body),
	}, nil
}

func check_ConsultantConfig(config ConnectorConfig) error {
	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.LicenseKey) == "" {
		return ErrNotConfigured
	}
	parsed, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ErrNotConfigured
	}
	return nil
}

func (connector ConsultantPlus) new_Request(ctx context.Context, method string, config ConnectorConfig, path string, body io.Reader) (*http.Request, error) {
	base := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	request, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, ErrNotConfigured
	}
	request.Header.Set("Authorization", "Bearer "+config.LicenseKey)
	if strings.TrimSpace(config.ClientID) != "" {
		request.Header.Set("X-Client-Id", strings.TrimSpace(config.ClientID))
	}
	request.Header.Set("Accept", "application/json, application/octet-stream")
	return request, nil
}

func (connector ConsultantPlus) do_JSON(request *http.Request, decodeTarget any) error {
	response, err := connector.client.Do(request)
	if err != nil {
		return ErrSourceUnreachable
	}
	defer response.Body.Close()
	if err := map_ConsultantStatus(response.StatusCode); err != nil {
		return err
	}
	if decodeTarget == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(decodeTarget); err != nil {
		return fmt.Errorf("%w: %v", ErrSourceUnreachable, err)
	}
	return nil
}

func map_ConsultantStatus(status int) error {
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrLicenseDenied
	}
	return ErrSourceUnreachable
}
