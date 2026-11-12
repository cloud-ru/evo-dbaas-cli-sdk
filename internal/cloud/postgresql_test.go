package cloud

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Имена с пробелом и / проверяют экранирование сегментов, ошибки API —
// возврат исходной причины, а не пустой успешный ответ. Сетка использует простые имена.
func TestPostgreSQLWireContract(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, method, path, body string
		call                     func(context.Context, *PostgreSQL) ([]byte, error)
	}{
		{"backups", "GET", "/v1/clusters/c%20%2F1/backups", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Backups(ctx, "c /1") }},
		{"backup", "GET", "/v1/clusters/c%20%2F1/backups/b%20%2F1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Backup(ctx, "c /1", "b /1") }},
		{"create backup", "POST", "/v1/clusters/c%20%2F1/backups", `{"cluster_id":"c /1","name":"daily"}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.CreateBackup(ctx, "c /1", "daily") }},
		{"suspend", "POST", "/v1/clusters/c%20%2F1:suspend", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.SuspendCluster(ctx, "c /1") }},
		{"resume", "POST", "/v1/clusters/c%20%2F1:resume", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.ResumeCluster(ctx, "c /1") }},
		{"switchover", "POST", "/v1/clusters/c%20%2F1:switchover", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.SwitchoverCluster(ctx, "c /1") }},
		{"storage info", "GET", "/v1/clusters/c%20%2F1:storageInfo", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.ClusterStorageInfo(ctx, "c /1") }},
		{"cluster locales", "GET", "/v1/clusters/c%20%2F1:locales", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.ClusterLocales(ctx, "c /1") }},
		{"version locales", "GET", "/v1/versions/v%20%2F1:locales?product_type=postgres", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.VersionLocales(ctx, "v /1") }},
		{"versions", "GET", "/v1/versions?product_type=postgres", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Versions(ctx) }},
		{"specs", "GET", "/v1/specifications?product_type=postgres&version_name=17", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.Specs(ctx, PostgreSQLSpecFilter{Version: "17"})
		}},
		{"specs cluster", "GET", "/v1/specifications?cluster_id=c1&product_type=postgres", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.Specs(ctx, PostgreSQLSpecFilter{Cluster: "c1"})
		}},
		{"clusters", "GET", "/v1/clusters?page_size=100&product_type=postgres&project_id=p1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Clusters(ctx, "p1") }},
		{"get cluster", "GET", "/v1/clusters/c%20%2F1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Cluster(ctx, "c /1") }},
		{"delete cluster", "DELETE", "/v1/clusters/c%20%2F1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.DeleteCluster(ctx, "c /1") }},
		{"operations", "GET", "/v1/clusters/c%20%2F1/operations?page_size=100", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.ClusterOperations(ctx, "c /1") }},
		{"operation", "GET", "/v1/operations/op%20%2F1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Operation(ctx, "op /1") }},
		{"databases", "GET", "/v1/clusters/c%20%2F1/databases", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Databases(ctx, "c /1") }},
		{"database", "GET", "/v1/clusters/c%20%2F1/databases/db%20%2F1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Database(ctx, "c /1", "db /1") }},
		{"create database", "POST", "/v1/clusters/c%20%2F1/databases", `{"cluster_id":"c /1","name":"db","owner":"u","lc_collate":"C","lc_ctype":"C","future":12345678901234567890}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.CreateDatabase(ctx, "c /1", PostgreSQLDatabaseCreate{Name: "db", Owner: "u", LCCollate: "C", LCCtype: "C", Body: []byte(`{"future":12345678901234567890}`)})
		}},
		{"delete database", "DELETE", "/v1/clusters/c%20%2F1/databases/db%20%2F1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.DeleteDatabase(ctx, "c /1", "db /1")
		}},
		{"users", "GET", "/v1/clusters/c%20%2F1/users", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.Users(ctx, "c /1") }},
		{"user", "GET", "/v1/clusters/c%20%2F1/users/u%20%2F1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.User(ctx, "c /1", "u /1") }},
		{"create user", "POST", "/v1/clusters/c%20%2F1/users", `{"cluster_id":"c /1","name":"u","password":"secret","future":true}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.CreateUser(ctx, "c /1", "u", "secret", []byte(`{"future":true}`))
		}},
		{"update user", "PUT", "/v1/clusters/c%20%2F1/users/u%20%2F1", `{"cluster_id":"c /1","name":"u /1","password":"new","granted_roles":[{"name":"reader"}],"future":true}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.UpdateUser(ctx, "c /1", "u /1", ptr("new"), []string{"reader"}, []byte(`{"future":true}`))
		}},
		{"clear roles", "PUT", "/v1/clusters/c%20%2F1/users/u%20%2F1", `{"cluster_id":"c /1","name":"u /1","granted_roles":[]}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.UpdateUser(ctx, "c /1", "u /1", nil, []string{}, nil)
		}},
		{"keep roles", "PUT", "/v1/clusters/c%20%2F1/users/u%20%2F1", `{"cluster_id":"c /1","name":"u /1","password":"new"}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.UpdateUser(ctx, "c /1", "u /1", ptr("new"), nil, nil)
		}},
		{"delete user", "DELETE", "/v1/clusters/c%20%2F1/users/u%20%2F1", "", func(ctx context.Context, p *PostgreSQL) ([]byte, error) { return p.DeleteUser(ctx, "c /1", "u /1") }},
		{"create cluster", "POST", "/v1/clusters", `{"project_id":"p","name":"n","version":"17","specification_id":"s","initial_database":"db","initial_database_lc_collate":"C","initial_database_lc_ctype":"C","instances":2,"subnet_ids":["a","b"],"storage":{"pg_data_gb":100,"pg_wal_gb":20},"logging":{"enabled":true,"log_group_id":"lg"},"description":"body","future":true}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.CreateCluster(ctx, PostgreSQLClusterCreate{Project: "p", Name: "n", Description: "flags", Version: "17", Spec: "s", Database: "db", LCCollate: "C", LCCtype: "C", Instances: 2, DataGiB: 100, WALGiB: 20, Subnets: []string{"a", "b"}, Logging: Logging{Enabled: true, GroupID: "lg"}, Body: []byte(`{"description":"body","future":true}`)})
		}},
		{"update cluster", "PATCH", "/v1/clusters/c%20%2F1", `{"cluster_id":"c /1","specification_id":"s2","instances":2,"subnet_ids":{"value":["a","b"]},"storage":{"pg_data_gb":100,"pg_wal_gb":20},"future":true}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.UpdateCluster(ctx, "c /1", PostgreSQLClusterUpdate{Spec: "s2", Instances: ptr(int32(2)), Subnets: ptr([]string{"a", "b"}), DataGiB: 100, WALGiB: 20, Body: []byte(`{"future":true}`)})
		}},
		{"update omitted", "PATCH", "/v1/clusters/c%20%2F1", `{"cluster_id":"c /1"}`, func(ctx context.Context, p *PostgreSQL) ([]byte, error) {
			return p.UpdateCluster(ctx, "c /1", PostgreSQLClusterUpdate{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, failure := range []bool{false, true} {
				var calls atomic.Int32

				p, err := NewPostgreSQL(serve(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					assert.Equal(t, tc.method, r.Method)
					expected, e := url.Parse(tc.path)
					assert.NoError(t, e)
					assert.Equal(t, expected.EscapedPath(), r.URL.EscapedPath())
					assert.Equal(t, expected.Query(), r.URL.Query())
					raw, e := io.ReadAll(r.Body)
					assert.NoError(t, e)

					if tc.body != "" {
						assert.JSONEq(t, tc.body, string(raw))
						assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					} else {
						assert.Empty(t, raw)
					}

					if failure {
						w.WriteHeader(http.StatusConflict)
						_, _ = io.WriteString(w, `{"message":"busy"}`)
					} else {
						_, _ = io.WriteString(w, `{"clusters":[{"future":12345678901234567890}],"operations":[{"future":12345678901234567890}],"id":"op1","done":true,"future":12345678901234567890}`)
					}
				}), noAuth)
				require.NoError(t, err)

				raw, err := tc.call(t.Context(), p)
				if failure {
					require.ErrorContains(t, err, "busy")
					assert.Nil(t, raw)
				} else {
					require.NoError(t, err)
					assert.Contains(t, string(raw), `12345678901234567890`)
				}

				assert.Equal(t, int32(1), calls.Load())
			}
		})
	}
}

func TestPostgreSQLInvalidBodyStopsWrites(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	p, err := NewPostgreSQL(serve(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) }), noAuth)
	require.NoError(t, err)

	for _, body := range []string{`broken`, `[]`} {
		for _, call := range []func() ([]byte, error){
			func() ([]byte, error) {
				return p.CreateCluster(t.Context(), PostgreSQLClusterCreate{Body: []byte(body)})
			},
			func() ([]byte, error) {
				return p.UpdateCluster(t.Context(), "c", PostgreSQLClusterUpdate{Body: []byte(body)})
			},
			func() ([]byte, error) {
				return p.CreateDatabase(t.Context(), "c", PostgreSQLDatabaseCreate{Body: []byte(body)})
			},
			func() ([]byte, error) { return p.CreateUser(t.Context(), "c", "u", "secret", []byte(body)) },
			func() ([]byte, error) { return p.UpdateUser(t.Context(), "c", "u", nil, nil, []byte(body)) },
		} {
			raw, err := call()
			require.Error(t, err)
			assert.Nil(t, raw)
		}
	}

	assert.Zero(t, calls.Load())
}

func TestPostgreSQLMergedTopology(t *testing.T) {
	t.Parallel()

	c := PostgreSQLClusterCreate{Version: "17", Spec: "flags", Instances: 1, Subnets: []string{"flags"}, Body: []byte(`{"specification_id":"body","version":"18","instances":2,"subnet_ids":["a","b"]}`)}
	top, err := c.Topology()
	require.NoError(t, err)
	assert.Equal(t, PostgreSQLTopology{Spec: "body", Version: "18", Instances: 2, Subnets: []string{"a", "b"}}, top)
	u := PostgreSQLClusterUpdate{Instances: ptr(int32(3)), Subnets: ptr([]string{"flags"}), Body: []byte(`{"specification_id":"s2","instances":1,"subnet_ids":{"value":["body"]}}`)}
	top, err = u.Topology(top)
	require.NoError(t, err)
	assert.Equal(t, PostgreSQLTopology{Spec: "s2", Version: "18", Instances: 1, Subnets: []string{"body"}}, top)

	for _, body := range []string{`broken`, `[]`, `{"instances":false}`} {
		c.Body = []byte(body)
		_, err := c.Topology()
		require.Error(t, err)

		u.Body = []byte(body)
		_, err = u.Topology(top)
		require.Error(t, err)
	}

	top, err = (PostgreSQLClusterUpdate{Body: []byte(`{"subnet_ids":{}}`)}).Topology(top)
	require.NoError(t, err)
	assert.Empty(t, top.Subnets)
	p, err := NewPostgreSQL(serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"op1","done":true,"resource_id":"c"}`)
	}), noAuth)
	require.NoError(t, err)
	op, err := p.Operations()(t.Context(), "op1")
	require.NoError(t, err)
	assert.Equal(t, "op1", op.ID)
	assert.True(t, op.Done)
	assert.Equal(t, "c", op.ResourceID)
}
