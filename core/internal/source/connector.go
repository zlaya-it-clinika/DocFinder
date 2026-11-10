package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"docfinder/core/internal/texts"
)

var (
	ErrUnknownKind       = errors.New("unknown source kind")
	ErrNotConfigured     = errors.New("source not configured")
	ErrLicenseDenied     = errors.New("source license denied")
	ErrSourceUnreachable = errors.New("source unreachable")
)

const (
	KindFolder             = "folder"
	KindDemo               = "demo"
	KindConsultantPlus     = "consultantplus"
	DefaultIntervalMinutes = 60
	MinIntervalMinutes     = 1
	MaxIntervalMinutes     = 7 * 24 * 60
)

type RemoteItem struct {
	ID       string
	Title    string
	Edition  string
	FileName string
}

type FetchedFile struct {
	ExternalID  string
	Title       string
	Edition     string
	FileName    string
	ContentType string
	Body        []byte
	Checksum    string
}

type ConnectorConfig struct {
	Path       string `json:"path,omitempty"`
	BaseURL    string `json:"baseUrl,omitempty"`
	ClientID   string `json:"clientId,omitempty"`
	LicenseKey string `json:"licenseKey,omitempty"`
	Bank       string `json:"bank,omitempty"`
	Rubric     string `json:"rubric,omitempty"`
}

type PublicConfig struct {
	Path          string `json:"path,omitempty"`
	BaseURL       string `json:"baseUrl,omitempty"`
	ClientID      string `json:"clientId,omitempty"`
	LicenseKeySet bool   `json:"licenseKeySet"`
	Bank          string `json:"bank,omitempty"`
	Rubric        string `json:"rubric,omitempty"`
}

type FieldInfo struct {
	Name   string `json:"name"`
	Secret bool   `json:"secret"`
}

type KindInfo struct {
	Kind   string      `json:"kind"`
	Title  string      `json:"title"`
	Lead   string      `json:"lead"`
	Fields []FieldInfo `json:"fields"`
}

type Connector interface {
	Kind() string
	List_Items(ctx context.Context, cursor string, config ConnectorConfig) (items []RemoteItem, nextCursor string, err error)
	Fetch_Item(ctx context.Context, item RemoteItem, config ConnectorConfig) (FetchedFile, error)
	Verify_Access(ctx context.Context, config ConnectorConfig) error
}

func Kind_Catalog() []KindInfo {
	return []KindInfo{
		{KindFolder, texts.SourceKindFolderTitle, texts.SourceKindFolderLead, []FieldInfo{{Name: "path"}}},
		{KindDemo, texts.SourceKindDemoTitle, texts.SourceKindDemoLead, []FieldInfo{}},
		{KindConsultantPlus, texts.SourceKindConsultantTitle, texts.SourceKindConsultantLead, []FieldInfo{
			{Name: "baseUrl"},
			{Name: "clientId"},
			{Name: "licenseKey", Secret: true},
			{Name: "bank"},
			{Name: "rubric"},
		}},
	}
}

func Known_Kind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case KindFolder, KindDemo, KindConsultantPlus:
		return true
	default:
		return false
	}
}

func Connector_ByKind(kind string) (Connector, error) {
	switch strings.TrimSpace(kind) {
	case KindFolder:
		return FolderConnector{}, nil
	case KindDemo:
		return DemoConnector{}, nil
	case KindConsultantPlus:
		return New_ConsultantPlus(nil), nil
	default:
		return nil, ErrUnknownKind
	}
}

func Checksum_Bytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func Normalize_Interval(minutes int) int {
	if minutes < MinIntervalMinutes {
		return DefaultIntervalMinutes
	}
	if minutes > MaxIntervalMinutes {
		return MaxIntervalMinutes
	}
	return minutes
}

func (config ConnectorConfig) Public() PublicConfig {
	return PublicConfig{
		Path:          strings.TrimSpace(config.Path),
		BaseURL:       strings.TrimSpace(config.BaseURL),
		ClientID:      strings.TrimSpace(config.ClientID),
		LicenseKeySet: strings.TrimSpace(config.LicenseKey) != "",
		Bank:          strings.TrimSpace(config.Bank),
		Rubric:        strings.TrimSpace(config.Rubric),
	}
}

func Merge_Config(saved, patch ConnectorConfig) ConnectorConfig {
	merged := saved
	if patch.Path != "" {
		merged.Path = strings.TrimSpace(patch.Path)
	}
	if patch.BaseURL != "" {
		merged.BaseURL = strings.TrimSpace(patch.BaseURL)
	}
	if patch.ClientID != "" {
		merged.ClientID = strings.TrimSpace(patch.ClientID)
	}
	if patch.LicenseKey != "" {
		merged.LicenseKey = patch.LicenseKey
	}
	if patch.Bank != "" {
		merged.Bank = strings.TrimSpace(patch.Bank)
	}
	if patch.Rubric != "" {
		merged.Rubric = strings.TrimSpace(patch.Rubric)
	}
	return merged
}

func Check_CanEnable(kind string, config ConnectorConfig) error {
	switch kind {
	case KindFolder:
		if strings.TrimSpace(config.Path) == "" {
			return ErrNotConfigured
		}
	case KindConsultantPlus:
		if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.LicenseKey) == "" {
			return ErrNotConfigured
		}
	case KindDemo:
		return nil
	default:
		return ErrUnknownKind
	}
	return nil
}

// List и Fetch одним проходом: курсор списка уходит обратно в источник
func Pull_Documents(ctx context.Context, connector Connector, cursor string, config ConnectorConfig) ([]FetchedFile, string, error) {
	items, nextCursor, err := connector.List_Items(ctx, cursor, config)
	if err != nil {
		return nil, "", err
	}
	files := make([]FetchedFile, 0, len(items))
	for _, item := range items {
		file, fetchError := connector.Fetch_Item(ctx, item, config)
		if fetchError != nil {
			return nil, "", fetchError
		}
		if strings.TrimSpace(file.Checksum) == "" {
			file.Checksum = Checksum_Bytes(file.Body)
		}
		files = append(files, file)
	}
	return files, nextCursor, nil
}
