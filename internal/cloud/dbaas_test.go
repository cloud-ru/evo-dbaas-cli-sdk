package cloud

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOperationRejectsMissingOperation(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", " \n\t", "not JSON", "null", `{}`, `{"done":true}`, `{"id":""}`} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			op, ok := ParseOperation([]byte(raw))
			assert.False(t, ok)
			assert.Equal(t, []byte(raw), op.Raw)
		})
	}
}

func TestParseOperationPreservesFieldsAndRawResponse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, raw, message string
		failed             bool
	}{
		{"успех", ` {"id":"op1","resource_id":"c1","description":"готово","done":true,"extra":12345678901234567890} `, "", false},
		{"ошибка с сообщением", `{"id":"op1","resource_id":"c1","description":"готово","done":true,"error":{"message":"нет места"}}`, "нет места", true},
		{"ошибка без сообщения", `{"id":"op1","resource_id":"c1","description":"готово","done":true,"error":{}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op, ok := ParseOperation([]byte(tc.raw))
			require.True(t, ok)
			assert.Equal(t, "op1", op.ID)
			assert.Equal(t, "c1", op.ResourceID)
			assert.Equal(t, "готово", op.Description)
			assert.True(t, op.Done)
			assert.Equal(t, tc.failed, op.Failed)
			assert.Equal(t, tc.message, op.Message)
			assert.Equal(t, []byte(tc.raw), op.Raw)
		})
	}
}

func TestAPIErrorExitCodeDistinguishesAccessDenial(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, message string
		status, want  int
	}{
		{"нет авторизации", "unauthorized", http.StatusUnauthorized, 4},
		{"нет доступа", "forbidden", http.StatusForbidden, 4},
		{"сбой проверки доступа", "rpc: failed to verify project access: unavailable", http.StatusForbidden, 1},
		{"401 с текстом сбоя — отказ, как в старом CLI", "failed to verify project access", http.StatusUnauthorized, 4},
		{"не найдено", "not found", http.StatusNotFound, 1},
		{"ошибка сервера", "internal", http.StatusInternalServerError, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, (&APIError{Status: tc.status, Message: tc.message}).ExitCode())
		})
	}
}

func TestRedisListsStopAtPageLimit(t *testing.T) {
	previous := maxPages
	t.Cleanup(func() { maxPages = previous })

	for _, endpoint := range redisListEndpoints() {
		t.Run(endpoint.key, func(t *testing.T) {
			for _, limit := range []int{0, 2} {
				t.Run(fmt.Sprint(limit), func(t *testing.T) {
					maxPages = limit

					var calls atomic.Int32

					r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
						n := int(calls.Add(1))

						token := ""
						if n > 1 {
							token = fmt.Sprint(n - 1)
						}

						checkRedisListRequest(t, req, endpoint, token)
						_, _ = fmt.Fprintf(w, `{"%s":[{"id":%d}],"next_page_token":"%d"}`, endpoint.key, n, n)
					}), noAuth)
					require.NoError(t, err)

					raw, err := endpoint.list(t.Context(), r)
					require.ErrorIs(t, err, ErrList)
					require.ErrorContains(t, err, "pages")
					assert.Nil(t, raw)
					assert.Equal(t, limit, int(calls.Load()))
					assert.LessOrEqual(t, int(calls.Load()), limit)
				})
			}
		})
	}
}

func TestRedisListsLimitCumulativeBytes(t *testing.T) {
	previous := maxListBytes
	t.Cleanup(func() { maxListBytes = previous })

	for _, endpoint := range redisListEndpoints() {
		t.Run(endpoint.key, func(t *testing.T) {
			first := fmt.Sprintf(`{"%s":[{"id":1}],"next_page_token":"next"}`, endpoint.key)

			last := fmt.Sprintf(`{"%s":[{"id":2}]}`, endpoint.key)
			for _, tc := range []struct {
				name    string
				limit   int
				calls   int
				wantErr bool
			}{
				{"первая страница больше предела", len(first) - 1, 1, true},
				{"сумма страниц больше предела", len(first) + len(last) - 1, 2, true},
				{"ровно предел", len(first) + len(last), 2, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					maxListBytes = tc.limit

					var calls atomic.Int32

					r, err := NewRedis(serve(t, func(w http.ResponseWriter, req *http.Request) {
						n := calls.Add(1)
						if n == 1 {
							checkRedisListRequest(t, req, endpoint, "")

							_, _ = w.Write([]byte(first))

							return
						}

						checkRedisListRequest(t, req, endpoint, "next")

						_, _ = w.Write([]byte(last))
					}), noAuth)
					require.NoError(t, err)

					raw, err := endpoint.list(t.Context(), r)
					if tc.wantErr {
						require.ErrorIs(t, err, ErrList)
						require.ErrorContains(t, err, "traversal stopped")
						assert.Nil(t, raw)
					} else {
						require.NoError(t, err)
						assert.JSONEq(t, fmt.Sprintf(`{"%s":[{"id":1},{"id":2}]}`, endpoint.key), string(raw))
					}

					assert.Equal(t, tc.calls, int(calls.Load()))
					assert.LessOrEqual(t, int(calls.Load()), tc.calls)
				})
			}
		})
	}
}
