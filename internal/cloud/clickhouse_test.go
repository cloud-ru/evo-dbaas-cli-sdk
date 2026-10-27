package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClickHouseMethodsUseRoutesQueriesAndBodies(t *testing.T) {
	t.Parallel()

	password := "secret"
	for _, tc := range []struct {
		name, method, path, body string
		query                    url.Values
		call                     func(context.Context, *ClickHouse) ([]byte, error)
	}{
		{"versions", "GET", "/v1/versions", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Versions(ctx) }},
		{"specs empty", "GET", "/v1/specifications", "", url.Values{"availability_zone_id": {"z1"}}, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Specs(ctx, "", "z1", "") }},
		{"specs server", "GET", "/v1/specifications", "", url.Values{"availability_zone_id": {"z1"}, "version": {"25.8"}, "role": {"SPECIFICATION_ROLE_CLICKHOUSE_SERVER"}}, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Specs(ctx, "25.8", "z1", "server") }},
		{"specs keeper", "GET", "/v1/specifications", "", url.Values{"availability_zone_id": {"z1"}, "role": {"SPECIFICATION_ROLE_CLICKHOUSE_KEEPER"}}, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Specs(ctx, "", "z1", "keeper") }},
		{"probe", "GET", "/v1/clusters", "", url.Values{"project_id": {"p1"}, "page_size": {"1"}}, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.ProbeProject(ctx, "p1") }},
		{"cluster", "GET", "/v1/clusters/c1", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Cluster(ctx, "c1") }},
		{"delete cluster", "DELETE", "/v1/clusters/c1", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.DeleteCluster(ctx, "c1") }},
		{"operation", "GET", "/v1/operations/op1", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Operation(ctx, "op1") }},
		{"databases", "GET", "/v1/clusters/c1/databases", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Databases(ctx, "c1") }},
		{"database", "GET", "/v1/clusters/c1/databases/db", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Database(ctx, "c1", "db") }},
		{"create database", "POST", "/v1/clusters/c1/databases", `{"cluster_id":"c1","name":"db"}`, nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.CreateDatabase(ctx, "c1", "db") }},
		{"delete database", "DELETE", "/v1/clusters/c1/databases/db", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.DeleteDatabase(ctx, "c1", "db") }},
		{"users", "GET", "/v1/clusters/c1/users", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Users(ctx, "c1") }},
		{"user", "GET", "/v1/clusters/c1/users/app", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.User(ctx, "c1", "app") }},
		{"create user empty roles", "POST", "/v1/clusters/c1/users", `{"cluster_id":"c1","user_name":"app","password":"secret"}`, nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) {
			return h.CreateUser(ctx, "c1", "app", password, []string{})
		}},
		{"create user roles", "POST", "/v1/clusters/c1/users", `{"cluster_id":"c1","user_name":"app","password":"secret","granted_roles":{"value":["reader","writer"]}}`, nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) {
			return h.CreateUser(ctx, "c1", "app", password, []string{"reader", "writer"})
		}},
		{"update user empty", "PATCH", "/v1/clusters/c1/users/app", `{"cluster_id":"c1","user_name":"app"}`, nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) {
			return h.UpdateUser(ctx, "c1", "app", nil, nil)
		}},
		{"update user password", "PATCH", "/v1/clusters/c1/users/app", `{"cluster_id":"c1","user_name":"app","password":"secret"}`, nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) {
			return h.UpdateUser(ctx, "c1", "app", &password, []string{})
		}},
		{"update user roles", "PATCH", "/v1/clusters/c1/users/app", `{"cluster_id":"c1","user_name":"app","granted_roles":{"value":["reader"]}}`, nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) {
			return h.UpdateUser(ctx, "c1", "app", nil, []string{"reader"})
		}},
		{"delete user", "DELETE", "/v1/clusters/c1/users/app", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.DeleteUser(ctx, "c1", "app") }},
		{"grant", "POST", "/v1/clusters/c1/users/app:grantRole", "", url.Values{"role": {"reader +/"}}, func(ctx context.Context, h *ClickHouse) ([]byte, error) {
			return h.GrantRole(ctx, "c1", "app", "reader +/")
		}},
		{"revoke", "POST", "/v1/clusters/c1/users/app:revokeRole", "", url.Values{"role": {"reader +/"}}, func(ctx context.Context, h *ClickHouse) ([]byte, error) {
			return h.RevokeRole(ctx, "c1", "app", "reader +/")
		}},
		{"roles", "GET", "/v1/clusters/c1/roles", "", nil, func(ctx context.Context, h *ClickHouse) ([]byte, error) { return h.Roles(ctx, "c1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusForbidden} {
				var calls atomic.Int32

				h, err := NewClickHouse(serve(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					assert.Equal(t, tc.method+" "+tc.path, r.Method+" "+r.URL.Path)

					query := tc.query
					if query == nil {
						query = url.Values{}
					}

					assert.Equal(t, query, r.URL.Query())
					b, e := io.ReadAll(r.Body)
					assert.NoError(t, e)

					if tc.body == "" {
						assert.Empty(t, b)
					} else {
						assert.JSONEq(t, tc.body, string(b))
						assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					}

					w.WriteHeader(status)

					switch status {
					case http.StatusForbidden:
						_, _ = io.WriteString(w, `{"message":"access denied"}`)
					case http.StatusOK:
						_, _ = io.WriteString(w, `{"future":12345678901234567890}`)
					}
				}), noAuth)
				require.NoError(t, err)

				raw, err := tc.call(t.Context(), h)
				if status == http.StatusForbidden {
					var ae *APIError
					require.ErrorAs(t, err, &ae)
					assert.Equal(t, status, ae.Status)
					assert.Equal(t, "access denied", ae.Message)
					assert.Nil(t, raw)
				} else {
					require.NoError(t, err)

					if status == http.StatusNoContent {
						assert.JSONEq(t, `{}`, string(raw))
					} else {
						assert.Equal(t, `{"future":12345678901234567890}`, string(raw))
					}
				}

				assert.Equal(t, int32(1), calls.Load())
			}
		})
	}
}

func TestClickHouseListsFollowTokensWithoutPageSize(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"clusters", "operations"} {
		for _, failure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/error=%v", key, failure), func(t *testing.T) {
				t.Parallel()

				var calls atomic.Int32

				h, err := NewClickHouse(serve(t, func(w http.ResponseWriter, r *http.Request) {
					n := calls.Add(1)
					path := "/v1/clusters"

					query := url.Values{}
					if key == "clusters" {
						query.Set("project_id", "p1")
					} else {
						path += "/c1/operations"
					}

					if n == 2 {
						query.Set("page_token", "next +/")
					}

					assert.Equal(t, "GET "+path, r.Method+" "+r.URL.Path)
					assert.Equal(t, query, r.URL.Query())
					b, e := io.ReadAll(r.Body)
					assert.NoError(t, e)
					assert.Empty(t, b)

					if n == 1 {
						_, _ = fmt.Fprintf(w, `{"%s":[{"id":1}],"next_page_token":"next +/"}`, key)
						return
					}

					if failure {
						w.WriteHeader(http.StatusNotFound)
						_, _ = io.WriteString(w, `{"message":"no route"}`)

						return
					}

					_, _ = fmt.Fprintf(w, `{"%s":[{"id":2}]}`, key)
				}), noAuth)
				require.NoError(t, err)

				var raw []byte
				if key == "clusters" {
					raw, err = h.Clusters(t.Context(), "p1")
				} else {
					raw, err = h.ClusterOperations(t.Context(), "c1")
				}

				if failure {
					var ae *APIError
					require.ErrorAs(t, err, &ae)
					assert.Equal(t, 404, ae.Status)
					assert.Nil(t, raw)
				} else {
					require.NoError(t, err)
					assert.JSONEq(t, fmt.Sprintf(`{"%s":[{"id":1},{"id":2}]}`, key), string(raw))
				}

				assert.Equal(t, int32(2), calls.Load())
			})
		}
	}
}

func TestClickHouseCreateClusterBody(t *testing.T) {
	t.Parallel()

	config, err := ParseClickHouseConfig("config", map[string]string{"max_connections": "100"})
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		change func(*ClickHouseClusterCreate)
		fields string
	}{
		{"omitted", func(*ClickHouseClusterCreate) {}, `{}`},
		{"nonpositive storage", func(c *ClickHouseClusterCreate) {
			c.StorageGiB = -1
			c.Keeper.StorageGiB = -1
			c.Subnets = []string{}
			c.Logging.GroupID = "ignored"
		}, `{"standalone_keeper":{}}`},
		{"full", func(c *ClickHouseClusterCreate) {
			c.Description = "description"
			c.StorageGiB = 40
			c.Subnets = []string{"sn1", "sn2"}
			c.KeeperMode = "standalone"
			c.Keeper = ClickHouseKeeper{Spec: "k1", StorageGiB: 10}
			c.Config = config
			c.Logging = Logging{Enabled: true, GroupID: "lg1"}
		}, `{"description":"description","storage_gib":40,"subnet_ids":["sn1","sn2"],"keeper_mode":"KEEPER_MODE_STANDALONE","standalone_keeper":{"specification_id":"k1","storage_gib":10},"config":{"max_connections":100},"logging":{"enabled":true,"log_group_id":"lg1"}}`},
		{"embedded and default logging", func(c *ClickHouseClusterCreate) { c.KeeperMode = "embedded"; c.Logging.Enabled = true }, `{"keeper_mode":"KEEPER_MODE_EMBEDDED","logging":{"enabled":true}}`},
		{"keeper spec", func(c *ClickHouseClusterCreate) { c.Keeper.Spec = "k1" }, `{"standalone_keeper":{"specification_id":"k1"}}`},
		{"keeper storage", func(c *ClickHouseClusterCreate) { c.Keeper.StorageGiB = 10 }, `{"standalone_keeper":{"storage_gib":10}}`},
		{"body replaces top level", func(c *ClickHouseClusterCreate) {
			c.Config = config
			c.Keeper = ClickHouseKeeper{Spec: "k1", StorageGiB: 10}
			c.Body = []byte(`{"name":"override","config":{"max_concurrent_queries":7},"standalone_keeper":{"storage_gib":99},"future":true}`)
		}, `{"name":"override","config":{"max_concurrent_queries":7},"standalone_keeper":{"storage_gib":99},"future":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := ClickHouseClusterCreate{Project: "p1", Name: "demo", Version: "25.8", Spec: "s1", Shards: 1, Replicas: 1}
			tc.change(&c)

			want := map[string]any{"project_id": "p1", "name": "demo", "version": "25.8", "specification_id": "s1", "shards": 1, "replicas": 1}

			var fields map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.fields), &fields))

			for k, v := range fields {
				want[k] = v
			}

			expected, err := json.Marshal(want)
			require.NoError(t, err)

			var calls atomic.Int32

			h, err := NewClickHouse(serve(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, "POST /v1/clusters", r.Method+" "+r.URL.Path)
				assert.Empty(t, r.URL.RawQuery)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				b, e := io.ReadAll(r.Body)
				assert.NoError(t, e)
				assert.JSONEq(t, string(expected), string(b))

				_, _ = io.WriteString(w, `{"id":"op1"}`)
			}), noAuth)
			require.NoError(t, err)
			raw, err := h.CreateCluster(t.Context(), c)
			require.NoError(t, err)
			assert.JSONEq(t, `{"id":"op1"}`, string(raw))
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestClickHouseUpdateClusterBody(t *testing.T) {
	t.Parallel()

	config, err := ParseClickHouseConfig("config", map[string]string{"max_connections": "100"})
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		update ClickHouseClusterUpdate
		want   string
	}{
		{"omitted", ClickHouseClusterUpdate{}, `{"cluster_id":"c1"}`},
		{"negative storage omitted", ClickHouseClusterUpdate{StorageGiB: -1}, `{"cluster_id":"c1"}`},
		{"full", ClickHouseClusterUpdate{Spec: "s2", StorageGiB: 80, Keeper: ClickHouseKeeper{Spec: "k2", StorageGiB: 20}, Config: config}, `{"cluster_id":"c1","specification_id":"s2","storage_gib":80,"standalone_keeper":{"specification_id":"k2","storage_gib":20},"config":{"max_connections":100}}`},
		{"body replaces", ClickHouseClusterUpdate{Spec: "s2", Config: config, Keeper: ClickHouseKeeper{Spec: "k2", StorageGiB: 20}, Body: []byte(`{"specification_id":"override","config":{"max_concurrent_queries":3},"standalone_keeper":{"storage_gib":99},"future":true}`)}, `{"cluster_id":"c1","specification_id":"override","config":{"max_concurrent_queries":3},"standalone_keeper":{"storage_gib":99},"future":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			h, err := NewClickHouse(serve(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, "PATCH /v1/clusters/c1", r.Method+" "+r.URL.Path)
				assert.Empty(t, r.URL.RawQuery)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				b, e := io.ReadAll(r.Body)
				assert.NoError(t, e)
				assert.JSONEq(t, tc.want, string(b))

				_, _ = io.WriteString(w, `{"id":"op1"}`)
			}), noAuth)
			require.NoError(t, err)
			raw, err := h.UpdateCluster(t.Context(), "c1", tc.update)
			require.NoError(t, err)
			assert.JSONEq(t, `{"id":"op1"}`, string(raw))
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestClickHouseRejectsInvalidBodyBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`broken`, `[]`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			h, err := NewClickHouse(serve(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(w, "unexpected request", http.StatusNotFound)
			}), noAuth)
			require.NoError(t, err)
			_, err = h.CreateCluster(t.Context(), ClickHouseClusterCreate{Body: []byte(body)})
			require.Error(t, err)
			_, err = h.UpdateCluster(t.Context(), "c1", ClickHouseClusterUpdate{Body: []byte(body)})
			require.Error(t, err)
			assert.Zero(t, calls.Load())
		})
	}
}

func TestParseClickHouseConfig(t *testing.T) {
	t.Parallel()

	c, err := ParseClickHouseConfig("config", map[string]string{"max_connections": "100", "max_concurrent_queries": "-2"})
	require.NoError(t, err)
	b, err := json.Marshal(c)
	require.NoError(t, err)
	assert.JSONEq(t, `{"max_connections":100,"max_concurrent_queries":-2}`, string(b))

	c, err = ParseClickHouseConfig("config", map[string]string{"k": "x"})
	require.EqualError(t, err, `--config k="x": an integer is required`)
	assert.Nil(t, c)
	c, err = ParseClickHouseConfig("config", map[string]string{"unknown": "1"})
	require.ErrorContains(t, err, "--config:")
	assert.Contains(t, err.Error(), `unknown field "unknown"`)
	assert.Nil(t, c)
}

func TestClickHouseOperationsPollsAndParsesResponse(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	h, err := NewClickHouse(serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "GET /v1/operations/op1", r.Method+" "+r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)

		_, _ = io.WriteString(w, `{"id":"op1","resource_id":"c1","done":true,"description":"created"}`)
	}), noAuth)
	require.NoError(t, err)
	op, err := h.Operations()(t.Context(), "op1")
	require.NoError(t, err)
	assert.Equal(t, "op1", op.ID)
	assert.Equal(t, "c1", op.ResourceID)
	assert.True(t, op.Done)
	assert.Equal(t, "created", op.Description)
	assert.Equal(t, int32(1), calls.Load())
}

// null в --body сейчас принимается как пустое наложение: поля запроса сохраняются.
func TestClickHouseNullBodyKeepsAssembledFields(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"POST", "PATCH"} {
		var calls atomic.Int32

		h, err := NewClickHouse(serve(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)

			path := "/v1/clusters"

			want := `{"project_id":"p1","name":"demo","version":"25.8","specification_id":"s1","shards":1,"replicas":1}`
			if method == "PATCH" {
				path += "/c1"
				want = `{"cluster_id":"c1","specification_id":"s2"}`
			}

			assert.Equal(t, method+" "+path, r.Method+" "+r.URL.Path)
			b, e := io.ReadAll(r.Body)
			assert.NoError(t, e)
			assert.JSONEq(t, want, string(b))

			_, _ = io.WriteString(w, `{"id":"op1"}`)
		}), noAuth)
		require.NoError(t, err)

		var raw []byte
		if method == "POST" {
			raw, err = h.CreateCluster(t.Context(), ClickHouseClusterCreate{Project: "p1", Name: "demo", Version: "25.8", Spec: "s1", Shards: 1, Replicas: 1, Body: []byte(`null`)})
		} else {
			raw, err = h.UpdateCluster(t.Context(), "c1", ClickHouseClusterUpdate{Spec: "s2", Body: []byte(`null`)})
		}

		require.NoError(t, err)
		assert.JSONEq(t, `{"id":"op1"}`, string(raw))
		assert.Equal(t, int32(1), calls.Load())
	}
}

func TestClickHouseClusterWritesPropagateAPIErrors(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"POST", "PATCH"} {
		var calls atomic.Int32

		h, err := NewClickHouse(serve(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)

			path := "/v1/clusters"
			if method == "PATCH" {
				path += "/c1"
			}

			assert.Equal(t, method+" "+path, r.Method+" "+r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"invalid cluster"}`)
		}), noAuth)
		require.NoError(t, err)

		var raw []byte
		if method == "POST" {
			raw, err = h.CreateCluster(t.Context(), ClickHouseClusterCreate{})
		} else {
			raw, err = h.UpdateCluster(t.Context(), "c1", ClickHouseClusterUpdate{})
		}

		var ae *APIError
		require.ErrorAs(t, err, &ae)
		assert.Equal(t, 400, ae.Status)
		assert.Equal(t, "invalid cluster", ae.Message)
		assert.Nil(t, raw)
		assert.Equal(t, int32(1), calls.Load())
	}
}
