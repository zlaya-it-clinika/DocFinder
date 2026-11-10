package httpapi

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/texts"
)

// Реквизиты организации в заглушке: в памяти до перезапуска, права - по ролям заглушки

type stubOrganization struct {
	stubStore    *stub
	mu           sync.Mutex
	officials    map[string]livestore.Official
	images       map[string]livestore.OrganizationImage
	nextOfficial int
}

func (stubStore *stub) create_Organization() *stubOrganization {
	organization := &stubOrganization{
		stubStore:    stubStore,
		officials:    map[string]livestore.Official{},
		images:       map[string]livestore.OrganizationImage{},
		nextOfficial: 1,
	}
	// Чату заглушки реквизиты нужны для макетов документов
	stubStore.organization = organization
	return organization
}

// Вошедший и подтвержденный пользователь, его флаги. Ошибки те же, что у боевого хранилища
func (stubStore *stub) authorize_Account(token string) ([]string, error) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record := stubStore.usersByID[stubStore.tokens[token]]
	if record == nil {
		return nil, livestore.ErrUnauthorized
	}
	if record.pending {
		return nil, livestore.ErrAccountPending
	}
	return stubStore.permissions_Of(record), nil
}

func (organization *stubOrganization) require_Manager(token string) error {
	permissions, err := organization.stubStore.authorize_Account(token)
	if err != nil {
		return err
	}
	if !auth.Has_Permission(permissions, texts.PermissionOrganizationManage) {
		return livestore.ErrAccessDenied
	}
	return nil
}

func (organization *stubOrganization) Read_Organization(ctx context.Context, token string) (livestore.Organization, error) {
	permissions, err := organization.stubStore.authorize_Account(token)
	if err != nil {
		return livestore.Organization{}, err
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	officials := organization.sorted_Officials()
	images := []livestore.OrganizationImageInfo{}
	for _, kind := range []string{imageKindEmblem, imageKindStamp} {
		if image, found := organization.images[kind]; found {
			images = append(images, livestore.OrganizationImageInfo{
				Kind:        image.Kind,
				FileName:    image.FileName,
				ContentType: image.ContentType,
				Size:        len(image.Body),
				UpdatedAt:   image.UpdatedAt,
			})
		}
	}
	return livestore.Organization{
		Officials: officials,
		Images:    images,
		CanManage: auth.Has_Permission(permissions, texts.PermissionOrganizationManage),
	}, nil
}

// Тот же порядок, что в базе: директор первым, дальше по должности и ФИО. Вызывается под organization.mu
func (organization *stubOrganization) sorted_Officials() []livestore.Official {
	officials := make([]livestore.Official, 0, len(organization.officials))
	for _, official := range organization.officials {
		officials = append(officials, official)
	}
	sort.Slice(officials, func(left, right int) bool {
		if officials[left].Chief != officials[right].Chief {
			return officials[left].Chief
		}
		if officials[left].Position != officials[right].Position {
			return officials[left].Position < officials[right].Position
		}
		return officials[left].FullName < officials[right].FullName
	})
	return officials
}

func (organization *stubOrganization) Create_Official(ctx context.Context, token string, input livestore.OfficialInput) (livestore.Official, error) {
	if err := organization.require_Manager(token); err != nil {
		return livestore.Official{}, err
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	officialID := "official-" + strconv.Itoa(organization.nextOfficial)
	organization.nextOfficial++
	return organization.save_Official(officialID, input), nil
}

func (organization *stubOrganization) Update_Official(ctx context.Context, token, officialID string, input livestore.OfficialInput) (livestore.Official, error) {
	if err := organization.require_Manager(token); err != nil {
		return livestore.Official{}, err
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	if _, found := organization.officials[officialID]; !found {
		return livestore.Official{}, livestore.ErrNotFound
	}
	return organization.save_Official(officialID, input), nil
}

// Вызывается под organization.mu. Новый директор снимает отметку с прежнего
func (organization *stubOrganization) save_Official(officialID string, input livestore.OfficialInput) livestore.Official {
	now := time.Now().UTC()
	if input.Chief {
		for id, official := range organization.officials {
			if official.Chief {
				official.Chief = false
				official.UpdatedAt = now
				organization.officials[id] = official
			}
		}
	}
	official := livestore.Official{
		OfficialID: officialID,
		Position:   input.Position,
		FullName:   input.FullName,
		Department: input.Department,
		Chief:      input.Chief,
		ValidFrom:  input.ValidFrom,
		UpdatedAt:  now,
	}
	organization.officials[officialID] = official
	return official
}

func (organization *stubOrganization) Delete_Official(ctx context.Context, token, officialID string) error {
	if err := organization.require_Manager(token); err != nil {
		return err
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	if _, found := organization.officials[officialID]; !found {
		return livestore.ErrNotFound
	}
	delete(organization.officials, officialID)
	return nil
}

func (organization *stubOrganization) Save_OrganizationImage(ctx context.Context, token string, image livestore.OrganizationImage) (livestore.OrganizationImageInfo, error) {
	if err := organization.require_Manager(token); err != nil {
		return livestore.OrganizationImageInfo{}, err
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	image.UpdatedAt = time.Now().UTC()
	organization.images[image.Kind] = image
	return livestore.OrganizationImageInfo{
		Kind:        image.Kind,
		FileName:    image.FileName,
		ContentType: image.ContentType,
		Size:        len(image.Body),
		UpdatedAt:   image.UpdatedAt,
	}, nil
}

func (organization *stubOrganization) Read_OrganizationImage(ctx context.Context, token, kind string) (livestore.OrganizationImage, error) {
	if _, err := organization.stubStore.authorize_Account(token); err != nil {
		return livestore.OrganizationImage{}, err
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	image, found := organization.images[kind]
	if !found {
		return livestore.OrganizationImage{}, livestore.ErrNotFound
	}
	return image, nil
}

func (organization *stubOrganization) Delete_OrganizationImage(ctx context.Context, token, kind string) error {
	if err := organization.require_Manager(token); err != nil {
		return err
	}
	organization.mu.Lock()
	defer organization.mu.Unlock()
	if _, found := organization.images[kind]; !found {
		return livestore.ErrNotFound
	}
	delete(organization.images, kind)
	return nil
}
