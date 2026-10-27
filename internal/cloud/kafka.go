package cloud

import (
	"context"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/gen/kafka"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// kafkaProduct — значение product_type. В спецификации параметр необязателен,
// но описан как «Укажите kafka», поэтому отправляем всегда.
const kafkaProduct = "kafka"

// Kafka — Public API Managed Kafka.
type Kafka struct{ c *kafka.Client }

// NewKafka — клиент API Kafka по адресу server.
func NewKafka(server string, o Options) (*Kafka, error) {
	c, err := kafka.NewClient(server, kafka.WithHTTPClient(o.HTTPClient), kafka.WithRequestEditorFn(kafka.RequestEditorFn(o.Auth)))
	if err != nil {
		return nil, i18n.Errorf("cloud.err.client_kafka", err)
	}

	return &Kafka{c: c}, nil
}

// KafkaSpecFilter — отбор спецификаций: версия и зона или кластер (для resize).
type KafkaSpecFilter struct {
	Version, Zone, Cluster string
}

// KafkaClusterCreate — новый кластер.
// Body — сырой JSON поверх собранного: ключи верхнего уровня заменяют поля.
type KafkaClusterCreate struct {
	Project, Name, Description, Version, Spec, Subnet string
	Brokers, StorageGiB                               int32
	UI, CruiseControl, AutoRebalance                  bool
	Params                                            map[string]string
	Logging                                           Logging
	Body                                              []byte
}

// KafkaClusterUpdate — изменение кластера; нулевые поля не отправляются.
type KafkaClusterUpdate struct {
	Spec       string
	StorageGiB int32
	Params     map[string]string
	Body       []byte
}

// KafkaTopicUpdate — изменение топика; нулевые поля не отправляются.
type KafkaTopicUpdate struct {
	Partitions, Replication int32
	Params                  map[string]string
}

func (k *Kafka) Versions(ctx context.Context) ([]byte, error) {
	return read(k.c.VersionServiceList(ctx, &kafka.VersionServiceListParams{ProductType: ptr(kafkaProduct)}))
}

func (k *Kafka) Specs(ctx context.Context, f KafkaSpecFilter) ([]byte, error) {
	return read(k.c.SpecificationServiceList(ctx, &kafka.SpecificationServiceListParams{
		VersionId: optional(f.Version), AvailabilityZoneId: optional(f.Zone), ClusterId: optional(f.Cluster),
	}))
}

// Clusters — все кластеры проекта: {"clusters": [...]} со всех страниц.
func (k *Kafka) Clusters(ctx context.Context, project string) ([]byte, error) {
	return pages("clusters", func(token string) ([]byte, error) {
		return read(k.c.ClusterServiceList(ctx, &kafka.ClusterServiceListParams{
			ProjectId: &project, PageSize: ptr(dbaasPageSize), PageToken: optional(token), ProductType: ptr(kafkaProduct),
		}))
	})
}

func (k *Kafka) Cluster(ctx context.Context, id string) ([]byte, error) {
	return read(k.c.ClusterServiceGet(ctx, id))
}

// CreateCluster возвращает операцию создания.
func (k *Kafka) CreateCluster(ctx context.Context, c KafkaClusterCreate) ([]byte, error) {
	req := kafka.CloudruDbaasKafkaV1CreateClusterRequest{
		ProjectId: c.Project, Name: c.Name, Description: optional(c.Description), VersionId: c.Version,
		SpecificationId: c.Spec, Brokers: c.Brokers, StorageGib: c.StorageGiB, SubnetId: c.Subnet,
		UiEnabled: c.UI, CruiseControlEnabled: c.CruiseControl, CruiseControlRebalanceAutoEnabled: c.AutoRebalance,
	}
	if len(c.Params) > 0 {
		req.Parameters = &c.Params
	}

	if c.Logging.Enabled {
		req.Logging = &kafka.CloudruDbaasKafkaV1LoggingConfig{Enabled: ptr(true), LogGroupId: optional(c.Logging.GroupID)}
	}

	body, err := mergeBody(req, c.Body)
	if err != nil {
		return nil, err
	}

	return read(k.c.ClusterServiceCreateWithBody(ctx, "application/json", body))
}

// UpdateCluster возвращает операцию изменения.
func (k *Kafka) UpdateCluster(ctx context.Context, id string, u KafkaClusterUpdate) ([]byte, error) {
	req := kafka.CloudruDbaasKafkaV1UpdateClusterRequest{ClusterId: id, SpecificationId: optional(u.Spec)}
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

	return read(k.c.ClusterServiceUpdateWithBody(ctx, id, "application/json", body))
}

// DeleteCluster возвращает операцию удаления.
func (k *Kafka) DeleteCluster(ctx context.Context, id string) ([]byte, error) {
	return read(k.c.ClusterServiceDelete(ctx, id))
}

// ClusterOperations — все операции кластера: {"operations": [...]}.
func (k *Kafka) ClusterOperations(ctx context.Context, id string) ([]byte, error) {
	return pages("operations", func(token string) ([]byte, error) {
		return read(k.c.ClusterServiceListOperations(ctx, id, &kafka.ClusterServiceListOperationsParams{
			PageSize: ptr(dbaasPageSize), PageToken: optional(token),
		}))
	})
}

func (k *Kafka) Operation(ctx context.Context, id string) ([]byte, error) {
	return read(k.c.OperationServiceGet(ctx, id))
}

// Operations — опрос операций Kafka для operation.Waiter.
func (k *Kafka) Operations() operation.Getter { return operationGetter(k.Operation) }

// Topics — все топики кластера: {"topics": [...]}; filter — часть имени.
func (k *Kafka) Topics(ctx context.Context, cluster, filter string) ([]byte, error) {
	return pages("topics", func(token string) ([]byte, error) {
		return read(k.c.TopicServiceList(ctx, cluster, &kafka.TopicServiceListParams{
			PageSize: ptr(int32(100)), PageToken: optional(token), FilterByPartialTopicName: optional(filter),
		}))
	})
}

func (k *Kafka) Topic(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(k.c.TopicServiceGet(ctx, cluster, name))
}

// CreateTopic возвращает операцию; params уходят всегда, пустые — «{}» (поле обязательно).
func (k *Kafka) CreateTopic(ctx context.Context, cluster, name string, partitions, replication int32, params map[string]string) ([]byte, error) {
	if params == nil {
		params = map[string]string{}
	}

	return read(k.c.TopicServiceCreate(ctx, cluster, kafka.CloudruDbaasKafkaV1CreateTopicRequest{
		ClusterId: cluster, Name: name, NumPartitions: partitions, ReplicationFactor: replication, Parameters: params,
	}))
}

// UpdateTopic возвращает операцию изменения.
func (k *Kafka) UpdateTopic(ctx context.Context, cluster, name string, u KafkaTopicUpdate) ([]byte, error) {
	req := kafka.CloudruDbaasKafkaV1UpdateTopicRequest{ClusterId: cluster, Name: name}
	if u.Partitions > 0 {
		req.NumPartitions = &u.Partitions
	}

	if u.Replication > 0 {
		req.ReplicationFactor = &u.Replication
	}

	if len(u.Params) > 0 {
		req.Parameters = &u.Params
	}

	return read(k.c.TopicServiceUpdate(ctx, cluster, name, req))
}

func (k *Kafka) DeleteTopic(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(k.c.TopicServiceDelete(ctx, cluster, name))
}

func (k *Kafka) Users(ctx context.Context, cluster string) ([]byte, error) {
	return read(k.c.UserServiceList(ctx, cluster))
}

func (k *Kafka) User(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(k.c.UserServiceGet(ctx, cluster, name))
}

// CreateUser возвращает операцию; пустой roles — поле не отправляется.
func (k *Kafka) CreateUser(ctx context.Context, cluster, name, password string, roles []string) ([]byte, error) {
	return read(k.c.UserServiceCreate(ctx, cluster, kafka.CloudruDbaasKafkaV1CreateUserRequest{
		ClusterId: cluster, Name: name, Password: password, GrantedRoles: kafkaRepeated(roles),
	}))
}

// UpdateUser возвращает операцию: nil password — не менять, пустой roles — не менять.
func (k *Kafka) UpdateUser(ctx context.Context, cluster, name string, password *string, roles []string) ([]byte, error) {
	return read(k.c.UserServiceUpdate(ctx, cluster, name, kafka.CloudruDbaasKafkaV1UpdateUserRequest{
		ClusterId: cluster, Name: name, Password: password, GrantedRoles: kafkaRepeated(roles),
	}))
}

func (k *Kafka) DeleteUser(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(k.c.UserServiceDelete(ctx, cluster, name))
}

func (k *Kafka) Roles(ctx context.Context, cluster string) ([]byte, error) {
	return read(k.c.RoleServiceList(ctx, cluster))
}

func kafkaRepeated(v []string) *kafka.CloudruProtobufV1RepeatedString {
	if len(v) == 0 {
		return nil
	}

	return &kafka.CloudruProtobufV1RepeatedString{Value: &v}
}
