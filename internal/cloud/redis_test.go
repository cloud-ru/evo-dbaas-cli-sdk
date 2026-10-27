package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

type redisListEndpoint struct {
	key, path, project string
	list               func(context.Context, *Redis) ([]byte, error)
}

func redisListEndpoints() []redisListEndpoint {
	return []redisListEndpoint{
		{"clusters", "/v1/clusters", "project1", func(ctx context.Context, r *Redis) ([]byte, error) {
			return r.Clusters(ctx, "project1")
		}},
		{"operations", "/v1/clusters/c1/operations", "", func(ctx context.Context, r *Redis) ([]byte, error) {
			return r.ClusterOperations(ctx, "c1")
		}},
	}
}

func checkRedisListRequest(t *testing.T, r *http.Request, endpoint redisListEndpoint, token string) {
	t.Helper()

	assert.Equal(t, http.MethodGet, r.Method)
	assert.Equal(t, endpoint.path, r.URL.Path)
	assert.Equal(t, []string{"100"}, r.URL.Query()["page_size"])
	assert.Equal(t, token, r.URL.Query().Get("page_token"))

	if token == "" {
		assert.NotContains(t, r.URL.Query(), "page_token")
	}

	if endpoint.project != "" {
		assert.Equal(t, []string{endpoint.project}, r.URL.Query()["project_id"])
	}
}

func TestRedisListsMergePagesWithoutLosingNumbers(t *testing.T) {
	t.Parallel()

	for _, endpoint := range redisListEndpoints() {
		t.Run(endpoint.key, func(t *testing.T) {
			t.Parallel()

			for _, tokenKey := range []string{"next_page_token", "nextPageToken"} {
				t.Run(tokenKey, func(t *testing.T) {
					t.Parallel()

					var calls atomic.Int32

					const token = "next +/="

					r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
						n := calls.Add(1)
						if n == 1 {
							checkRedisListRequest(t, req, endpoint, "")
							_, _ = fmt.Fprintf(w, `{"%s":[{"id":12345678901234567890,"extra":{"keep":true}}],"%s":%q}`, endpoint.key, tokenKey, token)

							return
						}

						assert.Equal(t, int32(2), n)
						checkRedisListRequest(t, req, endpoint, token)
						_, _ = fmt.Fprintf(w, `{"%s":[{"id":2}]}`, endpoint.key)
					}), noAuth)
					require.NoError(t, err)

					raw, err := endpoint.list(t.Context(), r)
					require.NoError(t, err)
					assert.JSONEq(t, fmt.Sprintf(`{"%s":[{"extra":{"keep":true},"id":12345678901234567890},{"id":2}]}`, endpoint.key), string(raw))
					assert.Equal(t, int32(2), calls.Load())
				})
			}
		})
	}
}

func TestRedisListsReturnEmptyArrayForEmptyObject(t *testing.T) {
	t.Parallel()

	for _, endpoint := range redisListEndpoints() {
		t.Run(endpoint.key, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				checkRedisListRequest(t, req, endpoint, "")

				_, _ = w.Write([]byte(`{}`))
			}), noAuth)
			require.NoError(t, err)

			raw, err := endpoint.list(t.Context(), r)
			require.NoError(t, err)
			assert.JSONEq(t, fmt.Sprintf(`{"%s":[]}`, endpoint.key), string(raw))
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestRedisListsRejectInvalidPages(t *testing.T) {
	t.Parallel()

	for _, endpoint := range redisListEndpoints() {
		t.Run(endpoint.key, func(t *testing.T) {
			t.Parallel()

			for _, tc := range []struct {
				name, body, message string
				calls               int32
			}{
				{"null вместо объекта", `null`, "null", 1},
				{"не JSON", `not JSON`, "cannot parse response", 1},
				{"нет поля на промежуточной странице", `{"next_page_token":"next"}`, "non-final page", 1},
				{"другое имя списка", `{"renamed":[]}`, "response format has changed", 1},
				{"поле не список", fmt.Sprintf(`{"%s":{}}`, endpoint.key), "not a list", 1},
				{"повтор токена", fmt.Sprintf(`{"%s":[{"id":1}],"next_page_token":"next"}`, endpoint.key), "looping", 2},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					var calls atomic.Int32

					r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
						n := calls.Add(1)

						token := ""
						if n > 1 {
							token = "next"
						}

						checkRedisListRequest(t, req, endpoint, token)

						_, _ = w.Write([]byte(tc.body))
					}), noAuth)
					require.NoError(t, err)

					raw, err := endpoint.list(t.Context(), r)
					require.ErrorIs(t, err, ErrList)
					require.ErrorContains(t, err, tc.message)
					assert.Nil(t, raw)
					assert.Equal(t, tc.calls, calls.Load())
				})
			}
		})
	}
}

func TestRedisListsReturnAPIErrorWithoutPartialResult(t *testing.T) {
	t.Parallel()

	for _, endpoint := range redisListEndpoints() {
		t.Run(endpoint.key, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
				if calls.Add(1) == 1 {
					checkRedisListRequest(t, req, endpoint, "")
					_, _ = fmt.Fprintf(w, `{"%s":[{"id":1}],"next_page_token":"next"}`, endpoint.key)

					return
				}

				checkRedisListRequest(t, req, endpoint, "next")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"unavailable"}`))
			}), noAuth)
			require.NoError(t, err)

			raw, err := endpoint.list(t.Context(), r)

			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusInternalServerError, apiErr.Status)
			assert.Equal(t, "unavailable", apiErr.Message)
			assert.Nil(t, raw)
			assert.Equal(t, int32(2), calls.Load())
		})
	}
}

type redisRoundTripFunc func(*http.Request) (*http.Response, error)

func (f redisRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRedisOperationsRetryTransientErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, message string
		status        int
		network       bool
	}{
		{"500", "internal", http.StatusInternalServerError, false},
		{"429", "too many requests", http.StatusTooManyRequests, false},
		{"сбой проверки доступа", "failed to verify project access", http.StatusForbidden, false},
		{"сбой сети", "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			networkErr := errors.New("сеть недоступна")

			const response = ` {"id":"op1","done":true,"resource_id":"c1","unknown":12345678901234567890} `

			url := serve(t, func(w http.ResponseWriter, req *http.Request) {
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, "/v1/operations/op1", req.URL.Path)

				if !tc.network && calls.Load() == 1 {
					w.WriteHeader(tc.status)
					_ = json.NewEncoder(w).Encode(map[string]string{"message": tc.message})

					return
				}

				_, _ = w.Write([]byte(response))
			})
			opts := noAuth
			opts.HTTPClient = &http.Client{Transport: redisRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 && tc.network {
					return nil, networkErr
				}

				return testTransport.RoundTrip(req)
			})}
			r, err := NewRedis(url, opts)
			require.NoError(t, err)

			retries := 0
			waiter := operation.NewWaiter(r.Operations(), operation.Options{
				Timeout: time.Second,
				Delay:   time.Millisecond,
				Retry: func(err error) {
					retries++
					cause := errors.Unwrap(err)
					require.Error(t, cause)
					assert.Equal(t, cause.Error(), err.Error())

					if tc.network {
						assert.ErrorIs(t, cause, networkErr)
					} else {
						apiErr, ok := cause.(*APIError)
						require.True(t, ok)
						assert.Equal(t, tc.status, apiErr.Status)
						assert.Equal(t, tc.message, apiErr.Message)
					}
				},
			})
			op, err := waiter.Wait(t.Context(), operation.Operation{ID: "op1"})
			require.NoError(t, err)
			assert.True(t, op.Done)
			assert.Equal(t, "op1", op.ID)
			assert.Equal(t, "c1", op.ResourceID)
			assert.JSONEq(t, response, string(op.Raw))
			assert.Equal(t, 1, retries)
			assert.Equal(t, int32(2), calls.Load())
		})
	}
}

func TestRedisOperationsStopOnPermanentOrEmptyResponse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body, message string
		status              int
	}{
		{"404", `{"message":"not found"}`, "not found", http.StatusNotFound},
		{"обычный отказ в доступе", `{"message":"forbidden"}`, "forbidden", http.StatusForbidden},
		{"пустое тело 200", "", "empty response for operation", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				assert.Equal(t, "/v1/operations/op1", req.URL.Path)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}), noAuth)
			require.NoError(t, err)

			_, err = operation.NewWaiter(r.Operations(), operation.Options{
				Timeout: time.Second,
				Delay:   time.Millisecond,
				Retry:   func(err error) { t.Errorf("неожиданный повтор: %v", err) },
			}).Wait(t.Context(), operation.Operation{ID: "op1"})

			var pending *operation.PendingError
			require.ErrorAs(t, err, &pending)
			require.Error(t, pending.Cause)
			require.ErrorContains(t, pending.Cause, tc.message)

			if tc.status != http.StatusOK {
				apiErr, ok := pending.Cause.(*APIError)
				require.True(t, ok)
				assert.Equal(t, tc.status, apiErr.Status)
			}

			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestRedisCreateClusterBuildsBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		change func(*RedisClusterCreate)
		fields string
	}{
		{"нулевые поля", func(*RedisClusterCreate) {}, `{}`},
		{"пустые коллекции", func(c *RedisClusterCreate) {
			c.Subnets, c.Params = []string{}, map[string]string{}
		}, `{}`},
		{"заполненные поля", func(c *RedisClusterCreate) {
			c.Description, c.StorageGiB = "описание", 16
			c.Subnets, c.Params = []string{"sn1", "sn2"}, map[string]string{"appendonly": "yes"}
		}, `{"description":"описание","storage_gib":16,"subnet_ids":["sn1","sn2"],"parameters":{"appendonly":"yes"}}`},
		{"логирование выключено", func(c *RedisClusterCreate) {
			c.Logging = Logging{GroupID: "lg1"}
		}, `{}`},
		{"логирование без группы", func(c *RedisClusterCreate) {
			c.Logging = Logging{Enabled: true}
		}, `{"logging":{"enabled":true}}`},
		{"логирование с группой", func(c *RedisClusterCreate) {
			c.Logging = Logging{Enabled: true, GroupID: "lg1"}
		}, `{"logging":{"enabled":true,"log_group_id":"lg1"}}`},
		{"Body заменяет ключ целиком и добавляет новый", func(c *RedisClusterCreate) {
			c.Params = map[string]string{"old": "value"}
			c.Body = []byte(`{"name":"overridden","parameters":{"new":"value"},"future":{"id":12345678901234567890}}`)
		}, `{"name":"overridden","parameters":{"new":"value"},"future":{"id":12345678901234567890}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := RedisClusterCreate{Project: "p1", Name: "redis1", Version: "v1", Spec: "s1", Type: "master-replica", Shards: 2, Replicas: 1}
			tc.change(&c)

			want := map[string]json.RawMessage{}
			require.NoError(t, json.Unmarshal([]byte(`{"project_id":"p1","name":"redis1","version_id":"v1","specification_id":"s1","type":"MASTER_REPLICA","shards":2,"replicas":1}`), &want))

			var extra map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(tc.fields), &extra))

			for key, value := range extra {
				want[key] = value
			}

			expected, err := json.Marshal(want)
			require.NoError(t, err)

			var calls atomic.Int32

			const response = ` {"id":"op1","unknown":12345678901234567890} `

			r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPost, req.Method)
				assert.Equal(t, "/v1/clusters", req.URL.Path)
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

				body, err := io.ReadAll(req.Body)
				if assert.NoError(t, err) {
					assert.JSONEq(t, string(expected), string(body))

					if len(c.Body) > 0 {
						assert.Contains(t, string(body), "12345678901234567890")
					}
				}

				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(response))
			}), noAuth)
			require.NoError(t, err)

			raw, err := r.CreateCluster(t.Context(), c)
			require.NoError(t, err)
			assert.JSONEq(t, response, string(raw))
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestRedisCreateClusterRejectsNonObjectBodyBeforeRequest(t *testing.T) {
	t.Parallel()

	// null — как в internal/cli: тело без изменений, не ошибка.
	for _, body := range []string{`[]`, `"text"`, `42`, `true`, `{"broken":`, ""} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			r, err := NewRedis(serve(t, func(_ http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				t.Errorf("запрос с недопустимым Body: %s", req.URL)
			}), noAuth)
			require.NoError(t, err)

			raw, err := r.CreateCluster(t.Context(), RedisClusterCreate{Body: []byte(body)})
			require.ErrorContains(t, err, "--body: a JSON object is required")
			assert.Nil(t, raw)
			assert.Zero(t, calls.Load())
		})
	}
}

func TestRedisUpdateClusterUsesPatchAndOmitsEmptyFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		u    RedisClusterUpdate
		want string
	}{
		{"нулевые поля", RedisClusterUpdate{}, `{"cluster_id":"c1"}`},
		{"пустые параметры", RedisClusterUpdate{Params: map[string]string{}}, `{"cluster_id":"c1"}`},
		{"заполненные поля", RedisClusterUpdate{Spec: "s2", StorageGiB: 32, Params: map[string]string{"appendonly": "yes"}}, `{"cluster_id":"c1","specification_id":"s2","storage_gib":32,"parameters":{"appendonly":"yes"}}`},
		{"Body поверх", RedisClusterUpdate{Spec: "s2", Body: []byte(`{"specification_id":"s3","future":true}`)}, `{"cluster_id":"c1","specification_id":"s3","future":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			const response = ` {"id":"update1","extra":true} `

			r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPatch, req.Method)
				assert.Equal(t, "/v1/clusters/c1", req.URL.Path)
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

				body, err := io.ReadAll(req.Body)
				if assert.NoError(t, err) {
					assert.JSONEq(t, tc.want, string(body))
				}

				_, _ = w.Write([]byte(response))
			}), noAuth)
			require.NoError(t, err)

			raw, err := r.UpdateCluster(t.Context(), "c1", tc.u)
			require.NoError(t, err)
			assert.JSONEq(t, response, string(raw))
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestRedisUpdateUserOmitsNilPassword(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		password *string
		want     string
	}{
		{"nil", nil, `{"cluster_id":"c1","user_name":"alice"}`},
		{"пустой пароль явно", ptr(""), `{"cluster_id":"c1","user_name":"alice","password":""}`},
		{"новый пароль", ptr("secret"), `{"cluster_id":"c1","user_name":"alice","password":"secret"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPut, req.Method)
				assert.Equal(t, "/v1/clusters/c1/users/alice", req.URL.Path)

				body, err := io.ReadAll(req.Body)
				if assert.NoError(t, err) {
					assert.JSONEq(t, tc.want, string(body))
				}

				_, _ = w.Write([]byte(`{"id":"op1"}`))
			}), noAuth)
			require.NoError(t, err)

			raw, err := r.UpdateUser(t.Context(), "c1", "alice", tc.password, nil)
			require.NoError(t, err)
			assert.JSONEq(t, `{"id":"op1"}`, string(raw))
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestRedisCreateUserWrapsOnlyNonEmptyRoles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		roles []string
		want  string
	}{
		{"nil", nil, `{"cluster_id":"c1","user_name":"alice","password":"secret"}`},
		{"пустой список", []string{}, `{"cluster_id":"c1","user_name":"alice","password":"secret"}`},
		{"с ролями", []string{"writer", "reader"}, `{"cluster_id":"c1","user_name":"alice","password":"secret","granted_roles":{"value":["writer","reader"]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPost, req.Method)
				assert.Equal(t, "/v1/clusters/c1/users", req.URL.Path)

				body, err := io.ReadAll(req.Body)
				if assert.NoError(t, err) {
					assert.JSONEq(t, tc.want, string(body))
				}

				_, _ = w.Write([]byte(`{"id":"op1"}`))
			}), noAuth)
			require.NoError(t, err)

			raw, err := r.CreateUser(t.Context(), "c1", "alice", "secret", tc.roles)
			require.NoError(t, err)
			assert.JSONEq(t, `{"id":"op1"}`, string(raw))
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}
