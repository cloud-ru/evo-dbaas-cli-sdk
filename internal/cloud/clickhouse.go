package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/gen/clickhouse"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// ClickHouse — Public API Managed ClickHouse.
type ClickHouse struct{ c *clickhouse.Client }

// NewClickHouse — клиент API ClickHouse по адресу server.
func NewClickHouse(server string, o Options) (*ClickHouse, error) {
	c, err := clickhouse.NewClient(server, clickhouse.WithHTTPClient(o.HTTPClient),
		clickhouse.WithRequestEditorFn(clickhouse.RequestEditorFn(o.Auth)))
	if err != nil {
		return nil, i18n.Errorf("cloud.err.client_clickhouse", err)
	}

	return &ClickHouse{c: c}, nil
}

// ClickHouseConfig — серверные параметры кластера.
type ClickHouseConfig = clickhouse.CloudruDbaasClickhouseV1ClusterConfig

// ParseClickHouseConfig — --config ключ=число. Неизвестный ключ отклоняется
// по полям структуры, а не молча теряется.
func ParseClickHouseConfig(flag string, kvs map[string]string) (*ClickHouseConfig, error) {
	nums := make(map[string]int64, len(kvs))
	for k, v := range kvs {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, errors.New(i18n.T("cloud.err.integer", flag, k, v))
		}

		nums[k] = n
	}

	raw, err := json.Marshal(nums)
	if err != nil {
		return nil, fmt.Errorf("--%s: %w", flag, err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var c ClickHouseConfig
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("--%s: %w", flag, err)
	}

	return &c, nil
}

// ClickHouseKeeper — выделенный Keeper (standalone_keeper); пустой — поле не отправляется.
type ClickHouseKeeper struct {
	Spec       string
	StorageGiB int32
}

func (k ClickHouseKeeper) value() *clickhouse.CloudruDbaasClickhouseV1StandaloneKeeper {
	if k.Spec == "" && k.StorageGiB == 0 {
		return nil
	}

	sk := &clickhouse.CloudruDbaasClickhouseV1StandaloneKeeper{SpecificationId: optional(k.Spec)}
	if k.StorageGiB > 0 {
		sk.StorageGib = &k.StorageGiB
	}

	return sk
}

// ClickHouseClusterCreate — новый кластер; KeeperMode — embedded | standalone | "" (legacy).
// Body — сырой JSON поверх собранного: ключи верхнего уровня заменяют поля.
type ClickHouseClusterCreate struct {
	Project, Name, Description, Version, Spec, KeeperMode string
	Shards, Replicas, StorageGiB                          int32
	Subnets                                               []string
	Keeper                                                ClickHouseKeeper
	Config                                                *ClickHouseConfig
	Logging                                               Logging
	Body                                                  []byte
}

// ClickHouseClusterUpdate — изменение кластера; нулевые поля не отправляются.
type ClickHouseClusterUpdate struct {
	Spec       string
	StorageGiB int32
	Keeper     ClickHouseKeeper
	Config     *ClickHouseConfig
	Body       []byte
}

func (h *ClickHouse) Versions(ctx context.Context) ([]byte, error) {
	return read(h.c.VersionServiceList(ctx))
}

// Specs — спецификации; role — server | keeper | "" (не отправлять).
func (h *ClickHouse) Specs(ctx context.Context, version, zone, role string) ([]byte, error) {
	p := &clickhouse.SpecificationServiceListParams{Version: optional(version), AvailabilityZoneId: &zone}
	if role != "" {
		p.Role = ptr(clickhouse.SpecificationServiceListParamsRole("SPECIFICATION_ROLE_CLICKHOUSE_" + strings.ToUpper(role)))
	}

	return read(h.c.SpecificationServiceList(ctx, p))
}

// Clusters — все кластеры проекта: {"clusters": [...]} со всех страниц.
// page_size не отправляем: ClickHouse отвечает и без него.
func (h *ClickHouse) Clusters(ctx context.Context, project string) ([]byte, error) {
	return pages("clusters", func(token string) ([]byte, error) {
		return read(h.c.ClusterServiceList(ctx, &clickhouse.ClusterServiceListParams{ProjectId: &project, PageToken: optional(token)}))
	})
}

// ProbeProject — первая страница из одного кластера: проверка доступа к проекту.
func (h *ClickHouse) ProbeProject(ctx context.Context, project string) ([]byte, error) {
	return read(h.c.ClusterServiceList(ctx, &clickhouse.ClusterServiceListParams{ProjectId: &project, PageSize: ptr("1")}))
}

func (h *ClickHouse) Cluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceGet(ctx, id))
}

// CreateCluster возвращает операцию создания.
func (h *ClickHouse) CreateCluster(ctx context.Context, c ClickHouseClusterCreate) ([]byte, error) {
	req := clickhouse.CloudruDbaasClickhouseV1CreateClusterRequest{
		ProjectId: c.Project, Name: c.Name, Description: optional(c.Description), Version: c.Version,
		SpecificationId: c.Spec, Shards: c.Shards, Replicas: c.Replicas, StandaloneKeeper: c.Keeper.value(), Config: c.Config,
	}
	if c.StorageGiB > 0 {
		req.StorageGib = &c.StorageGiB
	}

	if len(c.Subnets) > 0 {
		req.SubnetIds = &c.Subnets
	}

	if c.KeeperMode != "" {
		req.KeeperMode = ptr(clickhouse.CloudruDbaasClickhouseV1CreateClusterRequestKeeperMode("KEEPER_MODE_" + strings.ToUpper(c.KeeperMode)))
	}

	if c.Logging.Enabled {
		req.Logging = &clickhouse.CloudruDbaasClickhouseV1LoggingConfig{Enabled: ptr(true), LogGroupId: optional(c.Logging.GroupID)}
	}

	body, err := mergeBody(req, c.Body)
	if err != nil {
		return nil, err
	}

	return read(h.c.ClusterServiceCreateWithBody(ctx, "application/json", body))
}

// UpdateCluster возвращает операцию изменения.
func (h *ClickHouse) UpdateCluster(ctx context.Context, id string, u ClickHouseClusterUpdate) ([]byte, error) {
	req := clickhouse.CloudruDbaasClickhouseV1UpdateClusterRequest{
		ClusterId: id, SpecificationId: optional(u.Spec), StandaloneKeeper: u.Keeper.value(), Config: u.Config,
	}
	if u.StorageGiB > 0 {
		req.StorageGib = &u.StorageGiB
	}

	body, err := mergeBody(req, u.Body)
	if err != nil {
		return nil, err
	}

	return read(h.c.ClusterServiceUpdateWithBody(ctx, id, "application/json", body))
}

// DeleteCluster возвращает операцию удаления.
func (h *ClickHouse) DeleteCluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceDelete(ctx, id))
}

// ClusterOperations — все операции кластера: {"operations": [...]}.
func (h *ClickHouse) ClusterOperations(ctx context.Context, id string) ([]byte, error) {
	return pages("operations", func(token string) ([]byte, error) {
		return read(h.c.ClusterServiceListOperations(ctx, id, &clickhouse.ClusterServiceListOperationsParams{PageToken: optional(token)}))
	})
}

func (h *ClickHouse) Operation(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.OperationServiceGet(ctx, id))
}

// Operations — опрос операций ClickHouse для operation.Waiter.
func (h *ClickHouse) Operations() operation.Getter { return operationGetter(h.Operation) }

func (h *ClickHouse) Databases(ctx context.Context, cluster string) ([]byte, error) {
	return read(h.c.DatabaseServiceList(ctx, cluster))
}

func (h *ClickHouse) Database(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.DatabaseServiceGet(ctx, cluster, name))
}

func (h *ClickHouse) CreateDatabase(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.DatabaseServiceCreate(ctx, cluster, clickhouse.CloudruDbaasClickhouseV1CreateDatabaseRequest{ClusterId: cluster, Name: name}))
}

func (h *ClickHouse) DeleteDatabase(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.DatabaseServiceDelete(ctx, cluster, name))
}

func (h *ClickHouse) Users(ctx context.Context, cluster string) ([]byte, error) {
	return read(h.c.UserServiceList(ctx, cluster))
}

func (h *ClickHouse) User(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.UserServiceGet(ctx, cluster, name))
}

// CreateUser — пустой roles: поле не отправляется.
func (h *ClickHouse) CreateUser(ctx context.Context, cluster, name, password string, roles []string) ([]byte, error) {
	return read(h.c.UserServiceCreate(ctx, cluster, clickhouse.CloudruDbaasClickhouseV1CreateUserRequest{
		ClusterId: cluster, UserName: name, Password: password, GrantedRoles: chRepeated(roles),
	}))
}

// UpdateUser — nil password: не менять, пустой roles: не менять.
func (h *ClickHouse) UpdateUser(ctx context.Context, cluster, name string, password *string, roles []string) ([]byte, error) {
	return read(h.c.UserServiceUpdate(ctx, cluster, name, clickhouse.CloudruDbaasClickhouseV1UpdateUserRequest{
		ClusterId: cluster, UserName: name, Password: password, GrantedRoles: chRepeated(roles),
	}))
}

func (h *ClickHouse) DeleteUser(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.UserServiceDelete(ctx, cluster, name))
}

func (h *ClickHouse) GrantRole(ctx context.Context, cluster, name, role string) ([]byte, error) {
	return read(h.c.UserServiceGrantRole(ctx, cluster, name, &clickhouse.UserServiceGrantRoleParams{Role: &role}))
}

func (h *ClickHouse) RevokeRole(ctx context.Context, cluster, name, role string) ([]byte, error) {
	return read(h.c.UserServiceRevokeRole(ctx, cluster, name, &clickhouse.UserServiceRevokeRoleParams{Role: &role}))
}

func (h *ClickHouse) Roles(ctx context.Context, cluster string) ([]byte, error) {
	return read(h.c.RoleServiceList(ctx, cluster))
}

func chRepeated(v []string) *clickhouse.CloudruProtobufV1RepeatedString {
	if len(v) == 0 {
		return nil
	}

	return &clickhouse.CloudruProtobufV1RepeatedString{Value: &v}
}
