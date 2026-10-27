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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// Методы без тела сохраняют неизвестные поля ответа и передают фильтры API.
func TestKafkaReadMethodsUseTheirRoutesAndQueries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, method, path string
		query              url.Values
		call               func(context.Context, *Kafka) ([]byte, error)
	}{
		{"версии", http.MethodGet, "/v1/versions", url.Values{"product_type": {"kafka"}}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Versions(ctx) }},
		{"спецификации без фильтров", http.MethodGet, "/v1/specifications", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Specs(ctx, KafkaSpecFilter{}) }},
		{"спецификации с фильтрами", http.MethodGet, "/v1/specifications", url.Values{"version_id": {"v1"}, "availability_zone_id": {"z1"}, "cluster_id": {"c1"}}, func(ctx context.Context, k *Kafka) ([]byte, error) {
			return k.Specs(ctx, KafkaSpecFilter{Version: "v1", Zone: "z1", Cluster: "c1"})
		}},
		{"кластер", http.MethodGet, "/v1/clusters/c1", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Cluster(ctx, "c1") }},
		{"удаление кластера", http.MethodDelete, "/v1/clusters/c1", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.DeleteCluster(ctx, "c1") }},
		{"operation", http.MethodGet, "/v1/operations/op1", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Operation(ctx, "op1") }},
		{"топик", http.MethodGet, "/v1/clusters/c1/topics/events", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Topic(ctx, "c1", "events") }},
		{"удаление топика", http.MethodDelete, "/v1/clusters/c1/topics/events", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.DeleteTopic(ctx, "c1", "events") }},
		{"пользователи", http.MethodGet, "/v1/clusters/c1/users", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Users(ctx, "c1") }},
		{"пользователь", http.MethodGet, "/v1/clusters/c1/users/app", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.User(ctx, "c1", "app") }},
		{"удаление пользователя", http.MethodDelete, "/v1/clusters/c1/users/app", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.DeleteUser(ctx, "c1", "app") }},
		{"роли", http.MethodGet, "/v1/clusters/c1/roles", url.Values{}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Roles(ctx, "c1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, status := range []int{http.StatusOK, http.StatusNotFound} {
				var calls atomic.Int32

				const response = ` {"id":"op1","future":12345678901234567890} `

				k, err := NewKafka(serve(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					assert.Equal(t, tc.method+" "+tc.path, r.Method+" "+r.URL.Path)
					assert.Equal(t, tc.query, r.URL.Query())
					body, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.Empty(t, body)
					w.WriteHeader(status)

					if status == http.StatusNotFound {
						_, _ = io.WriteString(w, `{"message":"no route"}`)
						return
					}

					_, _ = io.WriteString(w, response)
				}), noAuth)
				require.NoError(t, err)

				raw, err := tc.call(t.Context(), k)
				if status == http.StatusOK {
					require.NoError(t, err)
					assert.JSONEq(t, response, string(raw))
				} else {
					var apiErr *APIError
					require.ErrorAs(t, err, &apiErr)
					assert.Equal(t, status, apiErr.Status)
					assert.Equal(t, "no route", apiErr.Message)
					assert.Nil(t, raw)
				}

				assert.Equal(t, int32(1), calls.Load())
			}
		})
	}
}

type kafkaListEndpoint struct {
	name, key, path string
	query           url.Values
	list            func(context.Context, *Kafka) ([]byte, error)
}

func kafkaListEndpoints() []kafkaListEndpoint {
	return []kafkaListEndpoint{
		{"кластеры", "clusters", "/v1/clusters", url.Values{"page_size": {"100"}, "project_id": {"p1"}, "product_type": {"kafka"}}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Clusters(ctx, "p1") }},
		{"операции", "operations", "/v1/clusters/c1/operations", url.Values{"page_size": {"100"}}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.ClusterOperations(ctx, "c1") }},
		{"топики без фильтра", "topics", "/v1/clusters/c1/topics", url.Values{"page_size": {"100"}}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Topics(ctx, "c1", "") }},
		{"топики с фильтром", "topics", "/v1/clusters/c1/topics", url.Values{"page_size": {"100"}, "filter_by_partial_topic_name": {"event +/"}}, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.Topics(ctx, "c1", "event +/") }},
	}
}

func TestKafkaListsMergePagesAndSendFiltersOnEveryPage(t *testing.T) {
	t.Parallel()

	for _, endpoint := range kafkaListEndpoints() {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			for _, tokenKey := range []string{"next_page_token", "nextPageToken"} {
				t.Run(tokenKey, func(t *testing.T) {
					t.Parallel()

					var calls atomic.Int32

					k, err := NewKafka(serve(t, func(w http.ResponseWriter, r *http.Request) {
						n := calls.Add(1)

						assert.Equal(t, "GET "+endpoint.path, r.Method+" "+r.URL.Path)

						query := endpoint.query.Clone()
						if n == 2 {
							query.Set("page_token", "next +/")
						}

						assert.Equal(t, query, r.URL.Query())

						if n == 1 {
							_, _ = fmt.Fprintf(w, `{"%s":[{"id":12345678901234567890,"future":true}],"%s":"next +/"}`, endpoint.key, tokenKey)
							return
						}

						_, _ = fmt.Fprintf(w, `{"%s":[{"id":2}]}`, endpoint.key)
					}), noAuth)
					require.NoError(t, err)
					raw, err := endpoint.list(t.Context(), k)
					require.NoError(t, err)
					assert.JSONEq(t, fmt.Sprintf(`{"%s":[{"id":12345678901234567890,"future":true},{"id":2}]}`, endpoint.key), string(raw))
					assert.Contains(t, string(raw), "12345678901234567890")
					assert.Equal(t, int32(2), calls.Load())
				})
			}
		})
	}
}

func TestKafkaListsRejectBrokenPagesAndDoNotReturnPartialResults(t *testing.T) {
	t.Parallel()

	for _, endpoint := range kafkaListEndpoints() {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			for _, tc := range []struct {
				name, body string
				apiError   bool
			}{
				{"ошибка API на второй странице", "", true},
				{"не JSON", "broken", false},
				{"not a list", fmt.Sprintf(`{"%s":42}`, endpoint.key), false},
				{"зацикливание", fmt.Sprintf(`{"%s":[],"next_page_token":"next"}`, endpoint.key), false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					var calls atomic.Int32

					k, err := NewKafka(serve(t, func(w http.ResponseWriter, _ *http.Request) {
						if calls.Add(1) == 1 {
							_, _ = fmt.Fprintf(w, `{"%s":[{"id":1}],"next_page_token":"next"}`, endpoint.key)
							return
						}

						if tc.apiError {
							w.WriteHeader(http.StatusNotFound)
							_, _ = io.WriteString(w, `{"message":"no route"}`)

							return
						}

						_, _ = io.WriteString(w, tc.body)
					}), noAuth)
					require.NoError(t, err)

					raw, err := endpoint.list(t.Context(), k)
					if tc.apiError {
						var apiErr *APIError
						require.ErrorAs(t, err, &apiErr)
						assert.Equal(t, http.StatusNotFound, apiErr.Status)
					} else {
						require.ErrorIs(t, err, ErrList)
					}

					assert.Nil(t, raw)
					assert.Equal(t, int32(2), calls.Load())
				})
			}
		})
	}
}

func TestKafkaCreateClusterBuildsBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		change func(*KafkaClusterCreate)
		fields string
	}{
		{"по умолчанию", func(*KafkaClusterCreate) {}, `{}`},
		{"пустые параметры", func(c *KafkaClusterCreate) { c.Params = map[string]string{} }, `{}`},
		{"заполненные поля", func(c *KafkaClusterCreate) {
			c.Description = "описание"
			c.UI, c.CruiseControl, c.AutoRebalance = true, true, true
			c.Params = map[string]string{"retention.ms": "1000"}
		}, `{"description":"описание","ui_enabled":true,"cruise_control_enabled":true,"cruise_control_rebalance_auto_enabled":true,"parameters":{"retention.ms":"1000"}}`},
		{"логи выключены", func(c *KafkaClusterCreate) { c.Logging = Logging{GroupID: "lg1"} }, `{}`},
		{"логи без группы", func(c *KafkaClusterCreate) { c.Logging = Logging{Enabled: true} }, `{"logging":{"enabled":true}}`},
		{"логи с группой", func(c *KafkaClusterCreate) { c.Logging = Logging{Enabled: true, GroupID: "lg1"} }, `{"logging":{"enabled":true,"log_group_id":"lg1"}}`},
		{"Body заменяет поля целиком", func(c *KafkaClusterCreate) {
			c.Params = map[string]string{"old": "value"}
			c.Logging = Logging{Enabled: true}
			c.Body = []byte(`{"name":"override","parameters":{"new":"value"},"logging":null,"ui_enabled":true,"future":12345678901234567890}`)
		}, `{"name":"override","parameters":{"new":"value"},"logging":null,"ui_enabled":true,"future":12345678901234567890}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := KafkaClusterCreate{Project: "p1", Name: "events", Version: "v1", Spec: "s1", Brokers: 3, StorageGiB: 40, Subnet: "sn1"}
			tc.change(&c)

			var want, extra map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(`{"project_id":"p1","name":"events","version_id":"v1","specification_id":"s1","brokers":3,"storage_gib":40,"subnet_id":"sn1","ui_enabled":false,"cruise_control_enabled":false,"cruise_control_rebalance_auto_enabled":false}`), &want))
			require.NoError(t, json.Unmarshal([]byte(tc.fields), &extra))

			for key, value := range extra {
				want[key] = value
			}

			expected, err := json.Marshal(want)
			require.NoError(t, err)
			checkKafkaWrite(t, http.MethodPost, "/v1/clusters", string(expected), func(ctx context.Context, k *Kafka) ([]byte, error) {
				return k.CreateCluster(ctx, c)
			})
		})
	}
}

// checkKafkaWrite проверяет всё тело, чтобы замечать и лишние поля.
func checkKafkaWrite(t *testing.T, method, path, want string, call func(context.Context, *Kafka) ([]byte, error)) {
	t.Helper()

	var calls atomic.Int32

	const response = ` {"id":"op1","future":12345678901234567890} `

	k, err := NewKafka(serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, method+" "+path, r.Method+" "+r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		if assert.NoError(t, err) {
			assert.JSONEq(t, want, string(body))
		}

		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, response)
	}), noAuth)
	require.NoError(t, err)
	raw, err := call(t.Context(), k)
	require.NoError(t, err)
	assert.JSONEq(t, response, string(raw))
	assert.Equal(t, int32(1), calls.Load())
}

func TestKafkaUpdateClusterOmitsEmptyFieldsAndMergesBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		u    KafkaClusterUpdate
		want string
	}{
		{"нулевые поля", KafkaClusterUpdate{}, `{"cluster_id":"c1"}`},
		{"пустая карта", KafkaClusterUpdate{Params: map[string]string{}}, `{"cluster_id":"c1"}`},
		{"отрицательный диск", KafkaClusterUpdate{StorageGiB: -1}, `{"cluster_id":"c1"}`},
		{"заполненные поля", KafkaClusterUpdate{Spec: "s2", StorageGiB: 80, Params: map[string]string{"x": "y"}}, `{"cluster_id":"c1","specification_id":"s2","storage_gib":80,"parameters":{"x":"y"}}`},
		{"Body поверх", KafkaClusterUpdate{Spec: "s2", Body: []byte(`{"specification_id":"s3","future":true}`)}, `{"cluster_id":"c1","specification_id":"s3","future":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkKafkaWrite(t, http.MethodPatch, "/v1/clusters/c1", tc.want, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.UpdateCluster(ctx, "c1", tc.u) })
		})
	}
}

func TestKafkaClusterWritesRejectInvalidBodyBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`[]`, `"text"`, `42`, `true`, `{"broken":`, ""} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			k, err := NewKafka(serve(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) }), noAuth)
			require.NoError(t, err)

			for _, call := range []func() ([]byte, error){
				func() ([]byte, error) { return k.CreateCluster(t.Context(), KafkaClusterCreate{Body: []byte(body)}) },
				func() ([]byte, error) {
					return k.UpdateCluster(t.Context(), "c1", KafkaClusterUpdate{Body: []byte(body)})
				},
			} {
				raw, err := call()
				require.ErrorContains(t, err, "--body: a JSON object is required")
				assert.Nil(t, raw)
			}

			assert.Zero(t, calls.Load())
		})
	}
}

func TestKafkaCreateTopicAlwaysSendsParameters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		params map[string]string
		want   string
	}{
		{"nil", nil, `{}`},
		{"пустая карта", map[string]string{}, `{}`},
		{"параметры", map[string]string{"retention.ms": "1000"}, `{"retention.ms":"1000"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkKafkaWrite(t, http.MethodPost, "/v1/clusters/c1/topics", `{"cluster_id":"c1","name":"events","num_partitions":3,"replication_factor":2,"parameters":`+tc.want+`}`, func(ctx context.Context, k *Kafka) ([]byte, error) {
				return k.CreateTopic(ctx, "c1", "events", 3, 2, tc.params)
			})
		})
	}
}

func TestKafkaUpdateTopicOmitsZeroFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		u    KafkaTopicUpdate
		want string
	}{
		{"нули", KafkaTopicUpdate{}, `{"cluster_id":"c1","name":"events"}`},
		{"пустая карта", KafkaTopicUpdate{Params: map[string]string{}}, `{"cluster_id":"c1","name":"events"}`},
		{"отрицательные числа", KafkaTopicUpdate{Partitions: -1, Replication: -1}, `{"cluster_id":"c1","name":"events"}`},
		{"изменения", KafkaTopicUpdate{Partitions: 5, Replication: 3, Params: map[string]string{"x": "y"}}, `{"cluster_id":"c1","name":"events","num_partitions":5,"replication_factor":3,"parameters":{"x":"y"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkKafkaWrite(t, http.MethodPatch, "/v1/clusters/c1/topics/events", tc.want, func(ctx context.Context, k *Kafka) ([]byte, error) { return k.UpdateTopic(ctx, "c1", "events", tc.u) })
		})
	}
}

func TestKafkaUserWritesUseNameAndWrapOnlyNonEmptyRoles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		roles  []string
		fields string
	}{
		{"nil", nil, ``},
		{"пустой список", []string{}, ``},
		{"роли", []string{"reader", "writer"}, `,"granted_roles":{"value":["reader","writer"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkKafkaWrite(t, http.MethodPost, "/v1/clusters/c1/users", `{"cluster_id":"c1","name":"app","password":"secret"`+tc.fields+`}`, func(ctx context.Context, k *Kafka) ([]byte, error) {
				return k.CreateUser(ctx, "c1", "app", "secret", tc.roles)
			})

			for _, password := range []*string{nil, ptr(""), ptr("new-secret")} {
				fields := tc.fields
				if password != nil {
					encoded, err := json.Marshal(*password)
					require.NoError(t, err)

					fields += `,"password":` + string(encoded)
				}

				checkKafkaWrite(t, http.MethodPatch, "/v1/clusters/c1/users/app", `{"cluster_id":"c1","name":"app"`+fields+`}`, func(ctx context.Context, k *Kafka) ([]byte, error) {
					return k.UpdateUser(ctx, "c1", "app", password, tc.roles)
				})
			}
		})
	}
}

func TestKafkaOperationsGetterWaitsForCompletion(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	k, err := NewKafka(serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET /v1/operations/op1", r.Method+" "+r.URL.Path)

		done := calls.Add(1) == 2
		_, _ = fmt.Fprintf(w, `{"id":"op1","done":%t,"resource_id":"c1","future":17}`, done)
	}), noAuth)
	require.NoError(t, err)
	op, err := operation.NewWaiter(k.Operations(), operation.Options{Timeout: time.Second, Delay: time.Millisecond}).Wait(t.Context(), operation.Operation{ID: "op1"})
	require.NoError(t, err)
	assert.True(t, op.Done)
	assert.Equal(t, "c1", op.ResourceID)
	assert.JSONEq(t, `{"id":"op1","done":true,"resource_id":"c1","future":17}`, string(op.Raw))
	assert.Equal(t, int32(2), calls.Load())
}
