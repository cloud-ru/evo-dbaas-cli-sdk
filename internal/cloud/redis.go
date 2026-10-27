package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/gen/redis"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// Redis — Public API Managed Redis.
type Redis struct{ c *redis.Client }

// NewRedis — клиент API Redis по адресу server.
func NewRedis(server string, o Options) (*Redis, error) {
	c, err := redis.NewClient(server, redis.WithHTTPClient(o.HTTPClient), redis.WithRequestEditorFn(redis.RequestEditorFn(o.Auth)))
	if err != nil {
		return nil, i18n.Errorf("cloud.err.client_redis", err)
	}

	return &Redis{c: c}, nil
}

// RedisSpecFilter — отбор спецификаций: версия и зона или кластер (для resize).
type RedisSpecFilter struct {
	Version, Zone, Cluster string
}

// RedisClusterCreate — новый кластер. Type — standalone | master-replica.
// Body — сырой JSON поверх собранного: ключи верхнего уровня заменяют поля.
type RedisClusterCreate struct {
	Project, Name, Description, Version, Spec, Type string
	Shards, Replicas, StorageGiB                    int32
	Subnets                                         []string
	Params                                          map[string]string
	Logging                                         Logging
	Body                                            []byte
}

// RedisClusterUpdate — изменение кластера; нулевые поля не отправляются.
type RedisClusterUpdate struct {
	Spec       string
	StorageGiB int32
	Params     map[string]string
	Body       []byte
}

// Logging — интеграция с Клиентским логированием; без Enabled поле не
// отправляется, пустая группа — по умолчанию.
type Logging struct {
	Enabled bool
	GroupID string
}

func (r *Redis) Versions(ctx context.Context) ([]byte, error) {
	return read(r.c.VersionServiceList(ctx))
}

func (r *Redis) Specs(ctx context.Context, f RedisSpecFilter) ([]byte, error) {
	return read(r.c.SpecificationServiceList(ctx, &redis.SpecificationServiceListParams{
		VersionId: optional(f.Version), AvailabilityZoneId: optional(f.Zone), ClusterId: optional(f.Cluster),
	}))
}

// Clusters — все кластеры проекта: {"clusters": [...]} со всех страниц.
func (r *Redis) Clusters(ctx context.Context, project string) ([]byte, error) {
	return pages("clusters", func(token string) ([]byte, error) {
		return read(r.c.ClusterServiceList(ctx, &redis.ClusterServiceListParams{
			ProjectId: &project, PageSize: ptr(dbaasPageSize), PageToken: optional(token),
		}))
	})
}

func (r *Redis) Cluster(ctx context.Context, id string) ([]byte, error) {
	return read(r.c.ClusterServiceGet(ctx, id))
}

// CreateCluster возвращает операцию создания.
func (r *Redis) CreateCluster(ctx context.Context, c RedisClusterCreate) ([]byte, error) {
	req := redis.CloudruDbaasRedisV1CreateClusterRequest{
		ProjectId: c.Project, Name: c.Name, Description: optional(c.Description), VersionId: c.Version,
		SpecificationId: c.Spec, Shards: c.Shards, Replicas: c.Replicas,
		Type: redis.CloudruDbaasRedisV1CreateClusterRequestType(strings.ReplaceAll(strings.ToUpper(c.Type), "-", "_")),
	}
	if c.StorageGiB > 0 {
		req.StorageGib = &c.StorageGiB
	}

	if len(c.Subnets) > 0 {
		req.SubnetIds = &c.Subnets
	}

	if len(c.Params) > 0 {
		req.Parameters = &c.Params
	}

	if c.Logging.Enabled {
		req.Logging = &redis.CloudruDbaasRedisV1LoggingConfig{Enabled: ptr(true), LogGroupId: optional(c.Logging.GroupID)}
	}

	body, err := mergeBody(req, c.Body)
	if err != nil {
		return nil, err
	}

	return read(r.c.ClusterServiceCreateWithBody(ctx, "application/json", body))
}

// UpdateCluster возвращает операцию изменения.
func (r *Redis) UpdateCluster(ctx context.Context, id string, u RedisClusterUpdate) ([]byte, error) {
	req := redis.CloudruDbaasRedisV1UpdateClusterRequest{ClusterId: id, SpecificationId: optional(u.Spec)}
	if u.StorageGiB > 0 {
		req.StorageGib = &u.StorageGiB
	}

	if len(u.Params) > 0 {
		req.Parameters = &u.Params
	}

	body, err := mergeBody(req, u.Body)
	if err != nil {
		return nil, err
	}

	return read(r.c.ClusterServiceUpdateWithBody(ctx, id, "application/json", body))
}

// DeleteCluster возвращает операцию удаления.
func (r *Redis) DeleteCluster(ctx context.Context, id string) ([]byte, error) {
	return read(r.c.ClusterServiceDelete(ctx, id))
}

// ClusterOperations — все операции кластера: {"operations": [...]}.
func (r *Redis) ClusterOperations(ctx context.Context, id string) ([]byte, error) {
	return pages("operations", func(token string) ([]byte, error) {
		return read(r.c.ClusterServiceListOperations(ctx, id, &redis.ClusterServiceListOperationsParams{
			PageSize: ptr(dbaasPageSize), PageToken: optional(token),
		}))
	})
}

func (r *Redis) Operation(ctx context.Context, id string) ([]byte, error) {
	return read(r.c.OperationServiceGet(ctx, id))
}

// Operations — опрос операций Redis для operation.Waiter.
func (r *Redis) Operations() operation.Getter { return operationGetter(r.Operation) }

func (r *Redis) Users(ctx context.Context, cluster string) ([]byte, error) {
	return read(r.c.UserServiceList(ctx, cluster))
}

// Пустое тело не подменяется на «{}»: user update перенёс бы пустой список ролей и обнулил права.
func (r *Redis) User(ctx context.Context, cluster, name string) ([]byte, error) {
	return readBody(r.c.UserServiceGet(ctx, cluster, name))
}

// CreateUser возвращает операцию; пустой roles — поле не отправляется.
func (r *Redis) CreateUser(ctx context.Context, cluster, name, password string, roles []string) ([]byte, error) {
	return read(r.c.UserServiceCreate(ctx, cluster, redis.CloudruDbaasRedisV1CreateUserRequest{
		ClusterId: cluster, UserName: name, Password: password, GrantedRoles: repeated(roles),
	}))
}

// UpdateUser заменяет пользователя целиком (PUT): nil password — не менять.
func (r *Redis) UpdateUser(ctx context.Context, cluster, name string, password *string, roles []string) ([]byte, error) {
	return read(r.c.UserServiceUpdate(ctx, cluster, name, redis.CloudruDbaasRedisV1UpdateUserRequest{
		ClusterId: cluster, UserName: name, Password: password, GrantedRoles: repeated(roles),
	}))
}

func (r *Redis) DeleteUser(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(r.c.UserServiceDelete(ctx, cluster, name))
}

func (r *Redis) Roles(ctx context.Context, cluster string) ([]byte, error) {
	return read(r.c.RoleServiceList(ctx, cluster))
}

func repeated(v []string) *redis.CloudruProtobufV1RepeatedString {
	if len(v) == 0 {
		return nil
	}

	return &redis.CloudruProtobufV1RepeatedString{Value: &v}
}

// mergeBody — тело запроса из собранной структуры и extra поверх: ключи
// верхнего уровня extra заменяют собранные. Так агенту доступно новое поле
// API в день выхода, до флага.
func mergeBody(req any, extra []byte) (io.Reader, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, i18n.Errorf("cloud.err.request_body", err)
	}

	if extra == nil {
		return bytes.NewReader(raw), nil
	}

	var base, over map[string]json.RawMessage
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, i18n.Errorf("cloud.err.request_body", err)
	}

	if err := json.Unmarshal(extra, &over); err != nil {
		return nil, i18n.Errorf("cloud.err.body_object", err)
	}

	for k, v := range over {
		base[k] = v
	}

	merged, err := json.Marshal(base)
	if err != nil {
		return nil, i18n.Errorf("cloud.err.request_body", err)
	}

	return bytes.NewReader(merged), nil
}
