package cloud

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/gen/iam"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// IAM — сервисные аккаунты, их ключи доступа и роли в проекте. Отвечает
// snake_case, хотя в спецификации camelCase, — тела ответов не разбираем
// сгенерированными типами (docs/api-coverage.md, «Расхождения»).
type IAM struct{ c *iam.Client }

// NewIAM — клиент IAM API по адресу server.
func NewIAM(server string, o Options) (*IAM, error) {
	c, err := iam.NewClient(server, iam.WithHTTPClient(o.HTTPClient), iam.WithRequestEditorFn(o.Auth),
		iam.WithRequestEditorFn(plusAsSpace))
	if err != nil {
		return nil, i18n.Errorf("cloud.err.client_iam", err)
	}

	return &IAM{c: c}, nil
}

// ServiceAccounts — сервисные аккаунты проекта, все страницы.
func (s *IAM) ServiceAccounts(ctx context.Context, project string) ([]byte, error) {
	return tokenPages("/api/v2/serviceAccounts", []string{"service_accounts", "serviceAccounts"}, func(token string) ([]byte, error) {
		return read(s.c.ServiceAccountServiceV2List(ctx, &iam.ServiceAccountServiceV2ListParams{
			TargetProjectId: &project, PageSize: ptr(strconv.Itoa(pageSize)), PageToken: optional(token),
		}))
	})
}

func (s *IAM) ServiceAccount(ctx context.Context, id string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.account"), id); err != nil {
		return nil, err
	}

	return read(s.c.ServiceAccountServiceV2Get(ctx, id))
}

// CreateServiceAccount — аккаунт name в проекте project; description "" — без описания.
func (s *IAM) CreateServiceAccount(ctx context.Context, project, name, description string) ([]byte, error) {
	return read(s.c.ServiceAccountServiceV2Create(ctx, iam.V2CreateServiceAccountRequest{
		Name: name, Description: optional(description), Target: iam.V2ServiceAccountTarget{ProjectId: &project},
	}))
}

func (s *IAM) DeleteServiceAccount(ctx context.Context, id string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.account"), id); err != nil {
		return nil, err
	}

	return read(s.c.ServiceAccountServiceV2Delete(ctx, id))
}

func (s *IAM) AccessKeys(ctx context.Context, account string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.account"), account); err != nil {
		return nil, err
	}

	return read(s.c.AccessKeyServiceList(ctx, account))
}

func (s *IAM) AccessKey(ctx context.Context, account, id string) ([]byte, error) {
	if err := errors.Join(pathID(i18n.T("cloud.resource.account"), account), pathID(i18n.T("cloud.resource.key"), id)); err != nil {
		return nil, err
	}

	return read(s.c.AccessKeyServiceGet(ctx, account, id))
}

// CreateAccessKey выпускает ключ; секрет — в ответе, один раз. Пустые
// description и ttl не отправляются.
func (s *IAM) CreateAccessKey(ctx context.Context, account, description, ttl string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.account"), account); err != nil {
		return nil, err
	}

	return read(s.c.AccessKeyServiceCreate(ctx, account, iam.V2AccessKeyServiceCreateBody{
		Description: optional(description), Ttl: optional(ttl),
	}))
}

func (s *IAM) DeleteAccessKey(ctx context.Context, account, id string) ([]byte, error) {
	if err := errors.Join(pathID(i18n.T("cloud.resource.account"), account), pathID(i18n.T("cloud.resource.key"), id)); err != nil {
		return nil, err
	}

	return read(s.c.AccessKeyServiceDelete(ctx, account, id))
}

// Permissions — выданные роли по фильтру IAM, все страницы.
func (s *IAM) Permissions(ctx context.Context, filter string) ([]byte, error) {
	return tokenPages("/api/v3/permissions", []string{"permissions"}, func(token string) ([]byte, error) {
		return read(s.c.PermissionServiceV3List(ctx, &iam.PermissionServiceV3ListParams{
			Filter: filter, PageSize: ptr(strconv.Itoa(pageSize)), PageToken: optional(token),
		}))
	})
}

func (s *IAM) Permission(ctx context.Context, id string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.role"), id); err != nil {
		return nil, err
	}

	return read(s.c.PermissionServiceV3Get(ctx, id))
}

// Grant выдаёт роль role субъекту subject в проекте project; expires — RFC 3339
// или "" (бессрочно).
func (s *IAM) Grant(ctx context.Context, project, subject, role, expires string) ([]byte, error) {
	req := iam.Iamv3CreatePermissionRequest{SubjectId: subject, ResourceId: project, Role: &role}
	if expires != "" {
		t, err := time.Parse(time.RFC3339, expires)
		if err != nil {
			return nil, i18n.Errorf("cloud.err.expires", expires, ErrBadExpires)
		}

		req.ExpiresAt = &t
	}

	return read(s.c.PermissionServiceV3Create(ctx, req))
}

func (s *IAM) Revoke(ctx context.Context, id string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.role"), id); err != nil {
		return nil, err
	}

	return read(s.c.PermissionServiceV3Delete(ctx, id))
}
