package livestore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/texts"
)

// Реквизиты организации для документов: генеральный директор, должностные лица, герб и штамп.
// Видят все вошедшие сотрудники (бланкам нужны подписант и картинки), меняет флаг organization.manage

const (
	counterOfficial = "official"
	// Отметка о выдаче флага organization.manage ролям установок, созданных до вкладки
	settingsKeyOrganizationPermission = "permission_organization_manage"
)

type Official struct {
	OfficialID string
	Position   string
	FullName   string
	Department string
	Chief      bool
	ValidFrom  string
	UpdatedAt  time.Time
}

// Поля должностного лица. Chief - генеральный директор: у прежнего директора отметка снимается
type OfficialInput struct {
	Position   string
	FullName   string
	Department string
	Chief      bool
	ValidFrom  string
}

type OrganizationImage struct {
	Kind        string
	FileName    string
	ContentType string
	Body        []byte
	UpdatedAt   time.Time
}

type OrganizationImageInfo struct {
	Kind        string
	FileName    string
	ContentType string
	Size        int
	UpdatedAt   time.Time
}

type Organization struct {
	Officials []Official
	Images    []OrganizationImageInfo
	CanManage bool
}

func (store *LiveStore) Read_Organization(ctx context.Context, token string) (Organization, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return Organization{}, err
	}
	officials, err := store.query_Officials(ctx)
	if err != nil {
		return Organization{}, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT kind, file_name, content_type, octet_length(body), updated_at
		FROM organization_images ORDER BY kind`)
	if err != nil {
		return Organization{}, err
	}
	defer rows.Close()
	images := []OrganizationImageInfo{}
	for rows.Next() {
		var image OrganizationImageInfo
		if err := rows.Scan(&image.Kind, &image.FileName, &image.ContentType, &image.Size, &image.UpdatedAt); err != nil {
			return Organization{}, err
		}
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return Organization{}, err
	}
	return Organization{Officials: officials, Images: images, CanManage: account.Has(texts.PermissionOrganizationManage)}, nil
}

// Директор первым, дальше по должности
func (store *LiveStore) query_Officials(ctx context.Context) ([]Official, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT official_id, position, full_name, department, chief, valid_from, updated_at
		FROM officials ORDER BY chief DESC, position, full_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	officials := []Official{}
	for rows.Next() {
		var official Official
		if err := rows.Scan(&official.OfficialID, &official.Position, &official.FullName, &official.Department, &official.Chief, &official.ValidFrom, &official.UpdatedAt); err != nil {
			return nil, err
		}
		officials = append(officials, official)
	}
	return officials, rows.Err()
}

func (store *LiveStore) Create_Official(ctx context.Context, token string, input OfficialInput) (Official, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionOrganizationManage); err != nil {
		return Official{}, err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Official{}, err
	}
	defer tx.Rollback(ctx)
	number, err := next_IDTx(ctx, tx, counterOfficial)
	if err != nil {
		return Official{}, err
	}
	officialID := "official-" + strconv.Itoa(number)
	if err := clear_ChiefTx(ctx, tx, input.Chief); err != nil {
		return Official{}, err
	}
	official, err := scan_Official(tx.QueryRow(ctx, `
		INSERT INTO officials (official_id, position, full_name, department, chief, valid_from)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING official_id, position, full_name, department, chief, valid_from, updated_at`,
		officialID, input.Position, input.FullName, input.Department, input.Chief, input.ValidFrom))
	if err != nil {
		return Official{}, err
	}
	return official, tx.Commit(ctx)
}

func (store *LiveStore) Update_Official(ctx context.Context, token, officialID string, input OfficialInput) (Official, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionOrganizationManage); err != nil {
		return Official{}, err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Official{}, err
	}
	defer tx.Rollback(ctx)
	if err := clear_ChiefTx(ctx, tx, input.Chief); err != nil {
		return Official{}, err
	}
	official, err := scan_Official(tx.QueryRow(ctx, `
		UPDATE officials
		SET position = $2, full_name = $3, department = $4, chief = $5, valid_from = $6, updated_at = now()
		WHERE official_id = $1
		RETURNING official_id, position, full_name, department, chief, valid_from, updated_at`,
		officialID, input.Position, input.FullName, input.Department, input.Chief, input.ValidFrom))
	if err != nil {
		return Official{}, err
	}
	return official, tx.Commit(ctx)
}

func (store *LiveStore) Delete_Official(ctx context.Context, token, officialID string) error {
	if _, err := store.require_Permission(ctx, token, texts.PermissionOrganizationManage); err != nil {
		return err
	}
	tag, err := store.pool.Exec(ctx, `DELETE FROM officials WHERE official_id = $1`, officialID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Новый генеральный директор снимает отметку с прежнего: уникальный индекс не даст двух
func clear_ChiefTx(ctx context.Context, tx pgx.Tx, chief bool) error {
	if !chief {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE officials SET chief = false, updated_at = now() WHERE chief`)
	return err
}

func scan_Official(row pgx.Row) (Official, error) {
	var official Official
	err := row.Scan(&official.OfficialID, &official.Position, &official.FullName, &official.Department, &official.Chief, &official.ValidFrom, &official.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Official{}, ErrNotFound
	}
	return official, err
}

func (store *LiveStore) Save_OrganizationImage(ctx context.Context, token string, image OrganizationImage) (OrganizationImageInfo, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionOrganizationManage); err != nil {
		return OrganizationImageInfo{}, err
	}
	info := OrganizationImageInfo{Kind: image.Kind, FileName: image.FileName, ContentType: image.ContentType, Size: len(image.Body)}
	err := store.pool.QueryRow(ctx, `
		INSERT INTO organization_images (kind, file_name, content_type, body)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (kind) DO UPDATE
		SET file_name = EXCLUDED.file_name, content_type = EXCLUDED.content_type, body = EXCLUDED.body, updated_at = now()
		RETURNING updated_at`,
		image.Kind, image.FileName, image.ContentType, image.Body).Scan(&info.UpdatedAt)
	if err != nil {
		return OrganizationImageInfo{}, err
	}
	return info, nil
}

func (store *LiveStore) Read_OrganizationImage(ctx context.Context, token, kind string) (OrganizationImage, error) {
	if _, err := store.Find_AccountByToken(ctx, token); err != nil {
		return OrganizationImage{}, err
	}
	image := OrganizationImage{Kind: kind}
	err := store.pool.QueryRow(ctx, `
		SELECT file_name, content_type, body, updated_at FROM organization_images WHERE kind = $1`, kind).Scan(
		&image.FileName, &image.ContentType, &image.Body, &image.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrganizationImage{}, ErrNotFound
	}
	if err != nil {
		return OrganizationImage{}, err
	}
	return image, nil
}

func (store *LiveStore) Delete_OrganizationImage(ctx context.Context, token, kind string) error {
	if _, err := store.require_Permission(ctx, token, texts.PermissionOrganizationManage); err != nil {
		return err
	}
	tag, err := store.pool.Exec(ctx, `DELETE FROM organization_images WHERE kind = $1`, kind)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
