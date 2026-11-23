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

// Имена с / и пробелом проверяют экранирование; mergeBody сохраняет неизвестное большое число.
// Сетка и метатест полноты используют простые имена и не проверяют ошибки каждой обёртки.
func TestOpenSearchWireContract(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, method, path, body string
		call                     func(context.Context, *OpenSearch) ([]byte, error)
	}{
		{"versions", "GET", "/v1/versions?product_type=opensearch", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.Versions(ctx) }},
		{"specs", "GET", "/v1/specifications?product_type=opensearch&version=2.18.0&availability_zone_id=az1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) {
			return h.Specs(ctx, OpenSearchSpecFilter{Version: "2.18.0", Zone: "az1"})
		}},
		// Живой прогон 17.11: cluster_id вместе с любым другим параметром — 400.
		{"specs by cluster", "GET", "/v1/specifications?cluster_id=c1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) {
			return h.Specs(ctx, OpenSearchSpecFilter{Cluster: "c1"})
		}},
		{"spec", "GET", "/v1/specifications/s%20%2F1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.Spec(ctx, "s /1") }},
		{"clusters", "GET", "/v1/clusters?page_size=50&product_type=opensearch&project_id=p1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.Clusters(ctx, "p1") }},
		{"cluster", "GET", "/v1/clusters/c%20%2F1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.Cluster(ctx, "c /1") }},
		{"delete cluster", "DELETE", "/v1/clusters/c%20%2F1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.DeleteCluster(ctx, "c /1") }},
		{"operations", "GET", "/v1/clusters/c%20%2F1/operations?page_size=50", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.ClusterOperations(ctx, "c /1") }},
		{"operation", "GET", "/v1/operations/op%20%2F1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.Operation(ctx, "op /1") }},
		{"snapshots", "GET", "/v1/clusters/c%20%2F1/snapshots", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.Snapshots(ctx, "c /1") }},
		{"snapshot", "GET", "/v1/clusters/c%20%2F1/snapshots/s%20%2F1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.Snapshot(ctx, "c /1", "s /1") }},
		{"create snapshot", "POST", "/v1/clusters/c%20%2F1/snapshots", `{"cluster_id":"c /1","snapshot_name":"daily","future":12345678901234567890}`, func(ctx context.Context, h *OpenSearch) ([]byte, error) {
			return h.CreateSnapshot(ctx, "c /1", "daily", []byte(`{"future":12345678901234567890}`))
		}},
		{"delete snapshot", "DELETE", "/v1/clusters/c%20%2F1/snapshots/s%20%2F1", "", func(ctx context.Context, h *OpenSearch) ([]byte, error) { return h.DeleteSnapshot(ctx, "c /1", "s /1") }},
		{"create cluster", "POST", "/v1/clusters", `{"project_id":"p1","name":"n","description":"body","version":"2.18.0","specification_id":"s","data_nodes":3,"master_nodes":3,"data_nodes_disk_size_gib":100,"master_nodes_disk_size_gib":25,"data_nodes_subnet_ids":["a","b"],"master_nodes_subnet_ids":["c","d"],"snapshots_schedule":{"enabled":true,"cron":"15 14 * * *","retention_days":"14"},"restore":{"enabled":true,"cluster_id":"source","snapshot_name":"daily"}}`, func(ctx context.Context, h *OpenSearch) ([]byte, error) {
			return h.CreateCluster(ctx, OpenSearchClusterCreate{Project: "p1", Name: "n", Description: "flags", Version: "2.18.0", Spec: "s", DataNodes: 3, DataGiB: 100, MasterNodes: ptr(int32(3)), MasterGiB: ptr(int32(25)), DataSubnets: []string{"a", "b"}, MasterSubnets: []string{"c", "d"}, Schedule: &OpenSearchSchedule{Enabled: true, Cron: ptr("15 14 * * *"), RetentionDays: ptr("14")}, Restore: &OpenSearchRestore{Enabled: true, ClusterID: ptr("source"), SnapshotName: ptr("daily")}, Body: []byte(`{"description":"body"}`)})
		}},
		{"update cluster", "PATCH", "/v1/clusters/c%20%2F1", `{"cluster_id":"c /1","specification_id":"s2","data_nodes":4,"master_nodes":3,"data_nodes_disk_size_gib":120,"master_nodes_disk_size_gib":50,"data_nodes_subnet_ids":{"value":["a","b"]},"master_nodes_subnet_ids":{"value":["c","d"]},"snapshots_schedule":{"enabled":false},"future":12345678901234567890}`, func(ctx context.Context, h *OpenSearch) ([]byte, error) {
			return h.UpdateCluster(ctx, "c /1", OpenSearchClusterUpdate{Spec: "s2", DataNodes: ptr(int32(4)), MasterNodes: ptr(int32(3)), DataGiB: ptr(int32(120)), MasterGiB: ptr(int32(50)), DataSubnets: ptr([]string{"a", "b"}), MasterSubnets: ptr([]string{"c", "d"}), Schedule: &OpenSearchSchedule{Enabled: false}, Body: []byte(`{"future":12345678901234567890}`)})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, status := range []int{http.StatusOK, http.StatusConflict} {
				var calls atomic.Int32

				h, err := NewOpenSearch(serve(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					assert.Equal(t, tc.method, r.Method)
					expected, e := url.Parse(tc.path)
					assert.NoError(t, e)
					assert.Equal(t, expected.EscapedPath(), r.URL.EscapedPath())
					assert.Equal(t, expected.Query(), r.URL.Query())
					body, e := io.ReadAll(r.Body)
					assert.NoError(t, e)

					if tc.body != "" {
						assert.JSONEq(t, tc.body, string(body))
						assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					} else {
						assert.Empty(t, body)
					}

					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"message":"wire","clusters":[{"new_field":12345678901234567890}],"operations":[{"new_field":12345678901234567890}],"new_field":12345678901234567890}`)
				}), noAuth)
				require.NoError(t, err)

				raw, err := tc.call(t.Context(), h)
				if status == http.StatusOK {
					require.NoError(t, err)
					assert.Contains(t, string(raw), `"new_field":12345678901234567890`)
				} else {
					require.ErrorContains(t, err, "wire")
					assert.Empty(t, raw)
				}

				assert.EqualValues(t, 1, calls.Load())
			}
		})
	}
}

// Ошибка --body должна возвращаться без запроса, в том числе из Topology до валидации.
func TestOpenSearchBodyErrors(t *testing.T) {
	t.Parallel()

	h, err := NewOpenSearch("https://example.invalid", Options{})
	require.NoError(t, err)

	for _, body := range []string{`[]`, `{"data_nodes":"bad"}`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			c := OpenSearchClusterCreate{Body: []byte(body)}
			u := OpenSearchClusterUpdate{Body: []byte(body)}
			_, err := c.Topology()
			require.Error(t, err)
			_, err = u.Topology(OpenSearchTopology{})
			require.Error(t, err)

			if body == `[]` {
				raw, err := h.CreateCluster(t.Context(), c)
				require.Error(t, err)
				assert.Empty(t, raw)
				raw, err = h.UpdateCluster(t.Context(), "c", u)
				require.Error(t, err)
				assert.Empty(t, raw)
				raw, err = h.CreateSnapshot(t.Context(), "c", "s", []byte(body))
				require.Error(t, err)
				assert.Empty(t, raw)
			}
		})
	}
}
