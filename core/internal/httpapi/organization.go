package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"docfinder/core/internal/livestore"
	"docfinder/core/internal/texts"
)

// Вкладка «Организация»: генеральный директор, должностные лица, герб и штамп для бланков.
// Обработчики общие для боевого режима и заглушки, разница только в хранилище

type organizationStore interface {
	Read_Organization(ctx context.Context, token string) (livestore.Organization, error)
	Create_Official(ctx context.Context, token string, input livestore.OfficialInput) (livestore.Official, error)
	Update_Official(ctx context.Context, token, officialID string, input livestore.OfficialInput) (livestore.Official, error)
	Delete_Official(ctx context.Context, token, officialID string) error
	Save_OrganizationImage(ctx context.Context, token string, image livestore.OrganizationImage) (livestore.OrganizationImageInfo, error)
	Read_OrganizationImage(ctx context.Context, token, kind string) (livestore.OrganizationImage, error)
	Delete_OrganizationImage(ctx context.Context, token, kind string) error
}

const (
	imageKindEmblem = "emblem"
	imageKindStamp  = "stamp"
	// Герб и штамп - небольшие картинки, больший файл скорее ошибка выбора
	maxOrganizationImageBytes = 2 << 20
	maxOfficialFieldRunes     = 200
)

var validFromDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Картинку принимаем по содержимому, а не по расширению: PNG или JPEG
var organizationImageTypes = map[string]bool{"image/png": true, "image/jpeg": true}

type officialRequest struct {
	Position   string `json:"position"`
	FullName   string `json:"fullName"`
	Department string `json:"department"`
	Chief      bool   `json:"chief"`
	ValidFrom  string `json:"validFrom"`
}

type officialResponse struct {
	OfficialID string `json:"officialId"`
	Position   string `json:"position"`
	FullName   string `json:"fullName"`
	Department string `json:"department"`
	Chief      bool   `json:"chief"`
	ValidFrom  string `json:"validFrom"`
	UpdatedAt  string `json:"updatedAt"`
}

type organizationImageResponse struct {
	Kind        string `json:"kind"`
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	UpdatedAt   string `json:"updatedAt"`
	Link        string `json:"link"`
}

// images: герб и штамп всегда в ответе, null - картинки нет
type organizationResponse struct {
	Officials []officialResponse                    `json:"officials"`
	Images    map[string]*organizationImageResponse `json:"images"`
	CanManage bool                                  `json:"canManage"`
}

func register_OrganizationRoutes(routeMux *http.ServeMux, store organizationStore) {
	api := organizationAPI{store: store}
	routeMux.HandleFunc("GET /organization", api.serve_Organization)
	routeMux.HandleFunc("POST /organization/officials", api.serve_CreateOfficial)
	routeMux.HandleFunc("PATCH /organization/officials/{officialId}", api.serve_UpdateOfficial)
	routeMux.HandleFunc("DELETE /organization/officials/{officialId}", api.serve_DeleteOfficial)
	routeMux.HandleFunc("GET /organization/images/{kind}", api.serve_Image)
	routeMux.HandleFunc("POST /organization/images/{kind}", api.serve_UploadImage)
	routeMux.HandleFunc("DELETE /organization/images/{kind}", api.serve_DeleteImage)
}

type organizationAPI struct {
	store organizationStore
}

func (api organizationAPI) serve_Organization(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	organization, err := api.store.Read_Organization(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_OrganizationResponse(organization))
}

func (api organizationAPI) serve_CreateOfficial(responseWriter http.ResponseWriter, request *http.Request) {
	input, ok := read_OfficialRequest(responseWriter, request)
	if !ok {
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	official, err := api.store.Create_Official(request.Context(), token, input)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_OfficialResponse(official))
}

func (api organizationAPI) serve_UpdateOfficial(responseWriter http.ResponseWriter, request *http.Request) {
	input, ok := read_OfficialRequest(responseWriter, request)
	if !ok {
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	official, err := api.store.Update_Official(request.Context(), token, request.PathValue("officialId"), input)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_OfficialResponse(official))
}

func (api organizationAPI) serve_DeleteOfficial(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if err := api.store.Delete_Official(request.Context(), token, request.PathValue("officialId")); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (api organizationAPI) serve_Image(responseWriter http.ResponseWriter, request *http.Request) {
	kind, ok := read_ImageKind(responseWriter, request)
	if !ok {
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	image, err := api.store.Read_OrganizationImage(request.Context(), token, kind)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	// Картинку показывает тег img на сайте: не скачиваем, а отдаем в окно
	responseWriter.Header().Set("Content-Type", image.ContentType)
	responseWriter.Header().Set("Cache-Control", "private, no-cache")
	responseWriter.WriteHeader(http.StatusOK)
	_, _ = responseWriter.Write(image.Body)
}

func (api organizationAPI) serve_UploadImage(responseWriter http.ResponseWriter, request *http.Request) {
	kind, ok := read_ImageKind(responseWriter, request)
	if !ok {
		return
	}
	fileName, body, _, err := read_UploadedFile(responseWriter, request)
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	contentType := http.DetectContentType(body)
	if len(body) == 0 || len(body) > maxOrganizationImageBytes || !organizationImageTypes[contentType] {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorImageInvalid)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	info, err := api.store.Save_OrganizationImage(request.Context(), token, livestore.OrganizationImage{
		Kind:        kind,
		FileName:    filepath.Base(fileName),
		ContentType: contentType,
		Body:        body,
	})
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_ImageResponse(info))
}

func (api organizationAPI) serve_DeleteImage(responseWriter http.ResponseWriter, request *http.Request) {
	kind, ok := read_ImageKind(responseWriter, request)
	if !ok {
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if err := api.store.Delete_OrganizationImage(request.Context(), token, kind); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

// Должность и ФИО обязательны, дата «действует с» - пустая или ГГГГ-ММ-ДД. false - ответ уже записан
func read_OfficialRequest(responseWriter http.ResponseWriter, request *http.Request) (livestore.OfficialInput, bool) {
	var body officialRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return livestore.OfficialInput{}, false
	}
	input := livestore.OfficialInput{
		Position:   strings.TrimSpace(body.Position),
		FullName:   strings.TrimSpace(body.FullName),
		Department: strings.TrimSpace(body.Department),
		Chief:      body.Chief,
		ValidFrom:  strings.TrimSpace(body.ValidFrom),
	}
	if !check_OfficialInput(input) {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return livestore.OfficialInput{}, false
	}
	return input, true
}

func check_OfficialInput(input livestore.OfficialInput) bool {
	if input.Position == "" || input.FullName == "" {
		return false
	}
	for _, value := range []string{input.Position, input.FullName, input.Department} {
		if utf8.RuneCountInString(value) > maxOfficialFieldRunes {
			return false
		}
	}
	return input.ValidFrom == "" || validFromDate.MatchString(input.ValidFrom)
}

func read_ImageKind(responseWriter http.ResponseWriter, request *http.Request) (string, bool) {
	kind := request.PathValue("kind")
	if kind != imageKindEmblem && kind != imageKindStamp {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return "", false
	}
	return kind, true
}

func build_OrganizationResponse(organization livestore.Organization) organizationResponse {
	response := organizationResponse{
		Officials: make([]officialResponse, 0, len(organization.Officials)),
		Images:    map[string]*organizationImageResponse{imageKindEmblem: nil, imageKindStamp: nil},
		CanManage: organization.CanManage,
	}
	for _, official := range organization.Officials {
		response.Officials = append(response.Officials, build_OfficialResponse(official))
	}
	for _, image := range organization.Images {
		built := build_ImageResponse(image)
		response.Images[image.Kind] = &built
	}
	return response
}

func build_OfficialResponse(official livestore.Official) officialResponse {
	return officialResponse{
		OfficialID: official.OfficialID,
		Position:   official.Position,
		FullName:   official.FullName,
		Department: official.Department,
		Chief:      official.Chief,
		ValidFrom:  official.ValidFrom,
		UpdatedAt:  official.UpdatedAt.UTC().Format(timeLayout),
	}
}

func build_ImageResponse(image livestore.OrganizationImageInfo) organizationImageResponse {
	return organizationImageResponse{
		Kind:        image.Kind,
		FileName:    image.FileName,
		ContentType: image.ContentType,
		Size:        image.Size,
		UpdatedAt:   image.UpdatedAt.UTC().Format(timeLayout),
		Link:        "/organization/images/" + image.Kind,
	}
}
