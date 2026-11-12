package cloud

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/gen/postgresql"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// PostgreSQL — Public API Managed PostgreSQL.
type PostgreSQL struct{ c *postgresql.Client }

func NewPostgreSQL(server string, o Options) (*PostgreSQL, error) {
	c, err := postgresql.NewClient(server, postgresql.WithHTTPClient(o.HTTPClient), postgresql.WithRequestEditorFn(postgresql.RequestEditorFn(o.Auth)))
	if err != nil {
		return nil, i18n.Errorf("cloud.err.client_postgresql", err)
	}

	return &PostgreSQL{c: c}, nil
}

func (h *PostgreSQL) Versions(ctx context.Context) ([]byte, error) {
	return read(h.c.VersionServiceList(ctx, &postgresql.VersionServiceListParams{ProductType: ptr("postgres")}))
}

type PostgreSQLSpecFilter struct{ Version, Cluster string }

func (h *PostgreSQL) Specs(ctx context.Context, f PostgreSQLSpecFilter) ([]byte, error) {
	return read(h.c.SpecificationServiceList(ctx, &postgresql.SpecificationServiceListParams{VersionName: optional(f.Version), ClusterId: optional(f.Cluster), ProductType: ptr("postgres")}))
}

func (h *PostgreSQL) Clusters(ctx context.Context, project string) ([]byte, error) {
	return pages("clusters", func(token string) ([]byte, error) {
		return read(h.c.ClusterServiceList(ctx, &postgresql.ClusterServiceListParams{ProjectId: &project, PageSize: ptr(dbaasPageSize), PageToken: optional(token), ProductType: ptr("postgres")}))
	})
}

func (h *PostgreSQL) Cluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceGet(ctx, id))
}

// PostgreSQLBackup — optional backup fields; nil preserves API defaults.
type PostgreSQLBackup struct {
	RetentionPolicyDays *int32  `json:"retention_policy_days,omitempty"`
	Schedule            *string `json:"schedule,omitempty"`
}

// PostgreSQLRecovery — restore into a new cluster.
type PostgreSQLRecovery struct {
	ClusterID string     `json:"cluster_id"`
	BackupID  string     `json:"backup_id,omitempty"`
	PITR      *time.Time `json:"pitr,omitempty"`
}

type PostgreSQLClusterCreate struct {
	Project, Name, Description, Version, Spec, Database, LCCollate, LCCtype string
	Instances, DataGiB, WALGiB                                              int32
	Subnets                                                                 []string
	Backup                                                                  *PostgreSQLBackup
	Recovery                                                                *PostgreSQLRecovery
	Logging                                                                 Logging
	Body                                                                    []byte
}
type PostgreSQLClusterUpdate struct {
	Backup          *PostgreSQLBackup
	Spec            string
	Instances       *int32
	DataGiB, WALGiB int32
	Subnets         *[]string
	Body            []byte
}

// PostgreSQLTopology — итоговые поля после mergeBody для проверки до запроса.
type PostgreSQLTopology struct {
	Spec      string              `json:"specification_id"`
	Version   string              `json:"version"`
	Instances int32               `json:"instances"`
	Subnets   []string            `json:"subnet_ids"`
	Legacy    json.RawMessage     `json:"subnet_id"`
	Recovery  *PostgreSQLRecovery `json:"recovery_spec"`
}

func (c PostgreSQLClusterCreate) Topology() (PostgreSQLTopology, error) {
	body, err := c.request()
	if err != nil {
		return PostgreSQLTopology{}, err
	}

	var top PostgreSQLTopology
	if err := json.NewDecoder(body).Decode(&top); err != nil {
		return top, i18n.Errorf("cloud.err.body_object", err)
	}

	return top, nil
}

// Topology применяет PATCH поверх текущей топологии; subnet_ids в PATCH — RepeatedString.
func (u PostgreSQLClusterUpdate) Topology(current PostgreSQLTopology) (PostgreSQLTopology, error) {
	body, err := u.request("")
	if err != nil {
		return current, err
	}

	var patch struct {
		Spec      *string                                     `json:"specification_id"`
		Instances *int32                                      `json:"instances"`
		Subnets   *postgresql.CloudruProtobufV1RepeatedString `json:"subnet_ids"`
		Legacy    json.RawMessage                             `json:"subnet_id"`
	}
	if err := json.NewDecoder(body).Decode(&patch); err != nil {
		return current, i18n.Errorf("cloud.err.body_object", err)
	}

	if patch.Spec != nil {
		current.Spec = *patch.Spec
	}

	if patch.Instances != nil {
		current.Instances = *patch.Instances
	}

	if patch.Subnets != nil {
		current.Subnets = nil
		if patch.Subnets.Value != nil {
			current.Subnets = *patch.Subnets.Value
		}
	}

	current.Legacy = patch.Legacy

	return current, nil
}

func (h *PostgreSQL) CreateCluster(ctx context.Context, c PostgreSQLClusterCreate) ([]byte, error) {
	body, err := c.request()
	if err != nil {
		return nil, err
	}

	return read(h.c.ClusterServiceCreateWithBody(ctx, "application/json", body))
}

func (h *PostgreSQL) UpdateCluster(ctx context.Context, id string, u PostgreSQLClusterUpdate) ([]byte, error) {
	body, err := u.request(id)
	if err != nil {
		return nil, err
	}

	return read(h.c.ClusterServiceUpdateWithBody(ctx, id, "application/json", body))
}

func (h *PostgreSQL) DeleteCluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceDelete(ctx, id))
}

func (h *PostgreSQL) ClusterOperations(ctx context.Context, id string) ([]byte, error) {
	return pages("operations", func(token string) ([]byte, error) {
		return read(h.c.ClusterServiceListOperations(ctx, id, &postgresql.ClusterServiceListOperationsParams{PageSize: ptr(dbaasPageSize), PageToken: optional(token)}))
	})
}

func (h *PostgreSQL) Operation(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.OperationServiceGet(ctx, id))
}
func (h *PostgreSQL) Operations() operation.Getter { return operationGetter(h.Operation) }
func (h *PostgreSQL) Databases(ctx context.Context, cluster string) ([]byte, error) {
	return read(h.c.DatabaseServiceList(ctx, cluster))
}

func (h *PostgreSQL) Database(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.DatabaseServiceGet(ctx, cluster, name))
}

type PostgreSQLDatabaseCreate struct {
	Name, Owner, LCCollate, LCCtype string
	Body                            []byte
}

func (h *PostgreSQL) CreateDatabase(ctx context.Context, cluster string, c PostgreSQLDatabaseCreate) ([]byte, error) {
	body, err := mergeBody(postgresql.CloudruDbaasPostgresqlV1CreateDatabaseRequest{ClusterId: cluster, Name: c.Name, Owner: c.Owner, LcCollate: optional(c.LCCollate), LcCtype: optional(c.LCCtype)}, c.Body)
	if err != nil {
		return nil, err
	}

	return read(h.c.DatabaseServiceCreateWithBody(ctx, cluster, "application/json", body))
}

func (h *PostgreSQL) DeleteDatabase(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.DatabaseServiceDelete(ctx, cluster, name))
}

func (h *PostgreSQL) Users(ctx context.Context, cluster string) ([]byte, error) {
	return read(h.c.UserServiceList(ctx, cluster))
}

func (h *PostgreSQL) User(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.UserServiceGet(ctx, cluster, name))
}

func (h *PostgreSQL) CreateUser(ctx context.Context, cluster, name, password string, extra []byte) ([]byte, error) {
	body, err := mergeBody(postgresql.CloudruDbaasPostgresqlV1CreateUserRequest{ClusterId: cluster, Name: name, Password: password}, extra)
	if err != nil {
		return nil, err
	}

	return read(h.c.UserServiceCreateWithBody(ctx, cluster, "application/json", body))
}

func (h *PostgreSQL) UpdateUser(ctx context.Context, cluster, name string, password *string, roles []string, extra []byte) ([]byte, error) {
	req := postgresql.CloudruDbaasPostgresqlV1UpdateUserRequest{ClusterId: cluster, Name: name, Password: password}
	if roles != nil {
		values := make([]postgresql.CloudruDbaasPostgresqlV1PostgreSQLRole, 0, len(roles))
		for _, role := range roles {
			values = append(values, postgresql.CloudruDbaasPostgresqlV1PostgreSQLRole{Name: ptr(role)})
		}

		req.GrantedRoles = &values
	}

	body, err := mergeBody(req, extra)
	if err != nil {
		return nil, err
	}

	return read(h.c.UserServiceUpdateWithBody(ctx, cluster, name, "application/json", body))
}

func (h *PostgreSQL) DeleteUser(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.UserServiceDelete(ctx, cluster, name))
}

func (c PostgreSQLClusterCreate) request() (io.Reader, error) {
	req := postgresql.CloudruDbaasPostgresqlV1CreateClusterRequest{
		Backup:    (*postgresql.CloudruDbaasPostgresqlV1BackupSpec)(c.Backup),
		ProjectId: c.Project, Name: c.Name, Description: optional(c.Description), Version: c.Version, SpecificationId: c.Spec, InitialDatabase: c.Database,
		InitialDatabaseLcCollate: optional(c.LCCollate), InitialDatabaseLcCtype: optional(c.LCCtype), Instances: &c.Instances, SubnetIds: &c.Subnets,
		Storage: postgresql.CloudruDbaasPostgresqlV1StorageSize{PgDataGb: c.DataGiB},
	}
	if c.Recovery != nil {
		req.RecoverySpec = &postgresql.CloudruDbaasPostgresqlV1RecoverySpec{ClusterId: c.Recovery.ClusterID, BackupId: optional(c.Recovery.BackupID), Pitr: c.Recovery.PITR}
	}

	if c.WALGiB > 0 {
		req.Storage.PgWalGb = &c.WALGiB
	}

	if c.Logging.Enabled {
		req.Logging = &postgresql.CloudruDbaasPostgresqlV1LoggingConfig{Enabled: ptr(true), LogGroupId: optional(c.Logging.GroupID)}
	}

	return mergeBody(req, c.Body)
}

func (u PostgreSQLClusterUpdate) request(id string) (io.Reader, error) {
	req := postgresql.CloudruDbaasPostgresqlV1UpdateClusterRequest{Backup: (*postgresql.CloudruDbaasPostgresqlV1BackupSpec)(u.Backup), ClusterId: id, SpecificationId: optional(u.Spec), Instances: u.Instances}
	if u.Subnets != nil {
		req.SubnetIds = &postgresql.CloudruProtobufV1RepeatedString{Value: u.Subnets}
	}

	if u.DataGiB > 0 || u.WALGiB > 0 {
		req.Storage = &postgresql.CloudruDbaasPostgresqlV1StorageSize{PgDataGb: u.DataGiB}
		if u.WALGiB > 0 {
			req.Storage.PgWalGb = &u.WALGiB
		}
	}

	return mergeBody(req, u.Body)
}

func (h *PostgreSQL) Backups(ctx context.Context, cluster string) ([]byte, error) {
	return read(h.c.BackupServiceList(ctx, cluster))
}

func (h *PostgreSQL) Backup(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.BackupServiceGet(ctx, cluster, name))
}

func (h *PostgreSQL) CreateBackup(ctx context.Context, cluster, name string) ([]byte, error) {
	return read(h.c.BackupServiceCreate(ctx, cluster, postgresql.CloudruDbaasPostgresqlV1CreateBackupRequest{ClusterId: cluster, Name: name}))
}

func (h *PostgreSQL) SuspendCluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceSuspend(ctx, id))
}

func (h *PostgreSQL) ResumeCluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceResume(ctx, id))
}

func (h *PostgreSQL) SwitchoverCluster(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceSwitchover(ctx, id))
}

func (h *PostgreSQL) ClusterStorageInfo(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceStorageInfo(ctx, id))
}

func (h *PostgreSQL) ClusterLocales(ctx context.Context, id string) ([]byte, error) {
	return read(h.c.ClusterServiceListLocales(ctx, id))
}

func (h *PostgreSQL) VersionLocales(ctx context.Context, version string) ([]byte, error) {
	return read(h.c.VersionServiceListLocales(ctx, version, &postgresql.VersionServiceListLocalesParams{ProductType: ptr("postgres")}))
}
