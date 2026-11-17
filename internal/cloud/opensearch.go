package cloud

import (
	"context"
	"encoding/json"
	"io"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/gen/opensearch"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// OpenSearch — Public API Managed OpenSearch.
type OpenSearch struct{ c *opensearch.Client }

func NewOpenSearch(server string, o Options) (*OpenSearch, error) {
	c, err := opensearch.NewClient(server, opensearch.WithHTTPClient(o.HTTPClient), opensearch.WithRequestEditorFn(opensearch.RequestEditorFn(o.Auth)))
	if err != nil {
		return nil, i18n.Errorf("cloud.err.client_opensearch", err)
	}

	return &OpenSearch{c: c}, nil
}

func (h *OpenSearch) Versions(ctx context.Context) ([]byte, error) {
	return read(h.c.VersionServiceList(ctx, &opensearch.VersionServiceListParams{ProductType: ptr("opensearch")}))
}

type OpenSearchSpecFilter struct{ Version, Zone, Cluster string }

// Specs — спецификации. С Cluster уходит только cluster_id: вместе с product_type, version
// или availability_zone_id API отвечает 400 «cluster_id and other parameters can't be specified
// at the same time» (живой прогон 17.11).
func (h *OpenSearch) Specs(ctx context.Context, f OpenSearchSpecFilter) ([]byte, error) {
	if f.Cluster != "" {
		return read(h.c.SpecificationServiceList(ctx, &opensearch.SpecificationServiceListParams{ClusterId: &f.Cluster}))
	}

	return read(h.c.SpecificationServiceList(ctx, &opensearch.SpecificationServiceListParams{ProductType: ptr("opensearch"), Version: optional(f.Version), AvailabilityZoneId: optional(f.Zone)}))
}

func (h *OpenSearch) Spec(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.SpecificationServiceGet(ctx, id))
}

func (h *OpenSearch) Clusters(ctx context.Context, project string) ([]byte, error) {
	return pages("clusters", func(token string) ([]byte, error) {
		return read(h.c.ClusterServiceList(ctx, &opensearch.ClusterServiceListParams{ProjectId: &project, ProductType: ptr("opensearch"), PageSize: ptr(dbaasPageSize), PageToken: optional(token)}))
	})
}

func (h *OpenSearch) Cluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceGet(ctx, id))
}

// OpenSearchSchedule — nil оставляет необязательное поле на усмотрение API.
type OpenSearchSchedule struct {
	Cron          *string `json:"cron,omitempty"`
	Enabled       bool    `json:"enabled"`
	RetentionDays *string `json:"retention_days,omitempty"`
}
type OpenSearchRestore struct {
	Enabled      bool    `json:"enabled"`
	ClusterID    *string `json:"cluster_id,omitempty"`
	SnapshotName *string `json:"snapshot_name,omitempty"`
}

// OpenSearchTopology — итоговые размеры после --body; nil означает, что поле не задано.
type OpenSearchTopology struct {
	Spec        string          `json:"specification_id"`
	DataNodes   *int32          `json:"data_nodes"`
	MasterNodes *int32          `json:"master_nodes"`
	DataGiB     *int32          `json:"data_nodes_disk_size_gib"`
	MasterGiB   *int32          `json:"master_nodes_disk_size_gib"`
	Legacy      json.RawMessage `json:"subnet_id"`
}

type OpenSearchClusterCreate struct {
	Project, Name, Description, Version, Spec string
	DataNodes, DataGiB                        int32
	MasterNodes, MasterGiB                    *int32
	DataSubnets, MasterSubnets                []string
	Schedule                                  *OpenSearchSchedule
	Restore                                   *OpenSearchRestore
	Body                                      []byte
}

type OpenSearchClusterUpdate struct {
	Spec                                       string
	DataNodes, MasterNodes, DataGiB, MasterGiB *int32
	DataSubnets, MasterSubnets                 *[]string
	Schedule                                   *OpenSearchSchedule
	Body                                       []byte
}

func (c OpenSearchClusterCreate) Topology() (OpenSearchTopology, error) {
	body, err := c.request()
	if err != nil {
		return OpenSearchTopology{}, err
	}

	var top OpenSearchTopology
	if err := json.NewDecoder(body).Decode(&top); err != nil {
		return top, i18n.Errorf("cloud.err.body_object", err)
	}

	return top, nil
}

// Topology накладывает PATCH на текущие размеры, сохраняя не переданные поля.
func (u OpenSearchClusterUpdate) Topology(current OpenSearchTopology) (OpenSearchTopology, error) {
	body, err := u.request("")
	if err != nil {
		return current, err
	}

	if err := json.NewDecoder(body).Decode(&current); err != nil {
		return current, i18n.Errorf("cloud.err.body_object", err)
	}

	return current, nil
}

func (h *OpenSearch) CreateCluster(ctx context.Context, c OpenSearchClusterCreate) ([]byte, error) {
	body, err := c.request()
	if err != nil {
		return nil, err
	}

	return read(h.c.ClusterServiceCreateWithBody(ctx, "application/json", body))
}

func (h *OpenSearch) UpdateCluster(ctx context.Context, id string, u OpenSearchClusterUpdate) ([]byte, error) {
	body, err := u.request(id)
	if err != nil {
		return nil, err
	}

	return read(h.c.ClusterServiceUpdateWithBody(ctx, id, "application/json", body))
}

func (h *OpenSearch) DeleteCluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceDelete(ctx, id))
}

func (h *OpenSearch) ClusterOperations(ctx context.Context, id string) ([]byte, error) {
	return pages("operations", func(token string) ([]byte, error) {
		return read(h.c.ClusterServiceListOperations(ctx, id, &opensearch.ClusterServiceListOperationsParams{PageSize: ptr(dbaasPageSize), PageToken: optional(token)}))
	})
}

func (h *OpenSearch) Operation(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.OperationServiceGet(ctx, id))
}
func (h *OpenSearch) Operations() operation.Getter { return operationGetter(h.Operation) }
func (h *OpenSearch) Snapshots(ctx context.Context, cluster string) ([]byte, error) {
	return read(h.c.SnapshotServiceList(ctx, cluster))
}

func (h *OpenSearch) Snapshot(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.SnapshotServiceGet(ctx, cluster, name))
}

func (h *OpenSearch) CreateSnapshot(ctx context.Context, cluster, name string, extra []byte) ([]byte, error) {
	body, err := mergeBody(opensearch.CloudruDbaasOpensearchV1CreateSnapshotRequest{ClusterId: cluster, SnapshotName: name}, extra)
	if err != nil {
		return nil, err
	}

	return read(h.c.SnapshotServiceCreateWithBody(ctx, cluster, "application/json", body))
}

func (h *OpenSearch) DeleteSnapshot(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.SnapshotServiceDelete(ctx, cluster, name))
}

func (c OpenSearchClusterCreate) request() (io.Reader, error) {
	req := opensearch.CloudruDbaasOpensearchV1CreateClusterRequest{
		ProjectId: c.Project, Name: c.Name, Description: optional(c.Description), Version: c.Version, SpecificationId: c.Spec,
		DataNodes: c.DataNodes, DataNodesDiskSizeGib: c.DataGiB, MasterNodes: c.MasterNodes, MasterNodesDiskSizeGib: c.MasterGiB,
		DataNodesSubnetIds: &c.DataSubnets, MasterNodesSubnetIds: &c.MasterSubnets,
		SnapshotsSchedule: (*opensearch.CloudruDbaasOpensearchV1SnapshotsSchedule)(c.Schedule),
	}
	if c.Restore != nil {
		req.Restore = &opensearch.CloudruDbaasOpensearchV1Restore{Enabled: c.Restore.Enabled, ClusterId: c.Restore.ClusterID, SnapshotName: c.Restore.SnapshotName}
	}

	return mergeBody(req, c.Body)
}

func (u OpenSearchClusterUpdate) request(id string) (io.Reader, error) {
	req := opensearch.CloudruDbaasOpensearchV1UpdateClusterRequest{
		ClusterId: id, SpecificationId: optional(u.Spec), DataNodes: u.DataNodes, MasterNodes: u.MasterNodes,
		DataNodesDiskSizeGib: u.DataGiB, MasterNodesDiskSizeGib: u.MasterGiB,
		SnapshotsSchedule: (*opensearch.CloudruDbaasOpensearchV1SnapshotsSchedule)(u.Schedule),
	}
	if u.DataSubnets != nil {
		req.DataNodesSubnetIds = &opensearch.CloudruProtobufV1RepeatedString{Value: u.DataSubnets}
	}

	if u.MasterSubnets != nil {
		req.MasterNodesSubnetIds = &opensearch.CloudruProtobufV1RepeatedString{Value: u.MasterSubnets}
	}

	return mergeBody(req, u.Body)
}
