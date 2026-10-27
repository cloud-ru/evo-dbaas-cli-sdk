package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var noDelays = []time.Duration{0, 0, 0}

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return srv.URL
}

// result — прочитанный ответ: тело закрыто внутри do.
type result struct {
	code    int
	body    string
	header  http.Header
	readErr error
}

func do(t *testing.T, rt http.RoundTripper, method, url string, body io.Reader) (result, error) {
	t.Helper()

	return doCtx(t, t.Context(), rt, method, url, body)
}

func doCtx(t *testing.T, ctx context.Context, rt http.RoundTripper, method, url string, body io.Reader) (result, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer old")

	resp, err := rt.RoundTrip(req)
	if err != nil {
		return result{}, fmt.Errorf("round trip: %w", err)
	}
	defer resp.Body.Close()

	raw, rerr := io.ReadAll(resp.Body)

	return result{code: resp.StatusCode, body: string(raw), header: resp.Header, readErr: rerr}, nil
}

func TestRetryGetCountsRetries(t *testing.T) {
	t.Parallel()

	for _, code := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		var hits atomic.Int64

		url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"message":"x"}`))
		})

		res, err := do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays}), http.MethodGet, url, nil)
		require.NoError(t, err)
		assert.Equal(t, code, res.code)
		assert.Equal(t, int64(1+len(noDelays)), hits.Load(), code)
	}
}

func TestRetryGetDefaultDelaysAreThree(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}, DefaultRetryDelays)
}

func TestRetryGetWaitsBetweenAttempts(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	start := time.Now()
	_, err := do(t, New(http.DefaultTransport, Options{RetryDelays: []time.Duration{40 * time.Millisecond, 60 * time.Millisecond}}),
		http.MethodGet, url, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(3), hits.Load())
	assert.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)
}

func TestRetryGetRecoversAfterTransient(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"failed to verify project access"}`))

			return
		}

		_, _ = w.Write([]byte(`{"id":"ok"}`))
	})

	res, err := do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays}), http.MethodGet, url, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.code)
	assert.Equal(t, int64(3), hits.Load())
}

func TestRetryGetNotOnRealForbiddenOrClientError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		code int
		body string
	}{
		{http.StatusForbidden, `{"message":"permission denied"}`},
		{http.StatusNotFound, `{"message":"not found"}`},
		{http.StatusBadRequest, `{"message":"bad"}`},
	} {
		var hits atomic.Int64

		url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte(tc.body))
		})

		res, err := do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays}), http.MethodGet, url, nil)
		require.NoError(t, err)

		assert.Equal(t, tc.body, res.body, "тело 403 после проверки возвращено на место")
		assert.Equal(t, int64(1), hits.Load(), tc.code)
	}
}

func TestRetryOnlyGet(t *testing.T) {
	t.Parallel()

	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		var hits atomic.Int64

		url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		})

		_, err := do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays}), m, url, strings.NewReader(`{}`))
		require.NoError(t, err)
		assert.Equal(t, int64(1), hits.Load(), m)
	}
}

func TestRetryGetStopsOnCancel(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := doCtx(t, ctx, New(http.DefaultTransport, Options{RetryDelays: []time.Duration{time.Hour}}), http.MethodGet, url, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Equal(t, int64(1), hits.Load())
}

func TestRefreshOn401RetriesGetOnceWithFreshToken(t *testing.T) {
	t.Parallel()

	var auths []string

	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"expired"}`))

			return
		}

		_, _ = w.Write([]byte(`{"id":"c1"}`))
	})

	refreshes := 0
	rt := New(http.DefaultTransport, Options{RetryDelays: noDelays, Refresh: func(context.Context) (string, error) {
		refreshes++
		return "fresh", nil
	}})

	res, err := do(t, rt, http.MethodGet, url, nil)
	require.NoError(t, err)

	assert.JSONEq(t, `{"id":"c1"}`, res.body)
	assert.Equal(t, 1, refreshes)
	assert.Equal(t, []string{"Bearer old", "Bearer fresh"}, auths)
}

// authEcho — базовый транспорт без сети: запоминает Authorization и отвечает
// 401 на всё, кроме свежего токена.
type authEcho struct{ auths []string }

func (b *authEcho) RoundTrip(req *http.Request) (*http.Response, error) {
	b.auths = append(b.auths, req.Header.Get("Authorization"))

	code := http.StatusOK
	if req.Header.Get("Authorization") != "Bearer fresh" {
		code = http.StatusUnauthorized
	}

	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"expired"}`)), Request: req}, nil
}

// Создание и правку после 401 не повторяем: токен
// перевыпущен, агент повторяет команду сам.
func TestRefreshOn401DoesNotRepeatWrites(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			base := &authEcho{}
			refreshes := 0
			rt := New(base, Options{RetryDelays: noDelays, Refresh: func(context.Context) (string, error) {
				refreshes++
				return "fresh", nil
			}})

			_, err := do(t, rt, method, "http://x/v1", strings.NewReader(`{"name":"c1"}`))
			require.ErrorContains(t, err, "repeat the command")
			assert.Equal(t, 1, refreshes, "токен перевыпущен для следующей команды")
			assert.Equal(t, []string{"Bearer old"}, base.auths)
		})
	}
}

func TestRefreshOn401OnlyOnce(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"no"}`))
	})

	refreshes := 0
	rt := New(http.DefaultTransport, Options{RetryDelays: noDelays, Refresh: func(context.Context) (string, error) {
		refreshes++
		return "fresh", nil
	}})

	res, err := do(t, rt, http.MethodGet, url, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.code)
	assert.Equal(t, 1, refreshes)
	assert.Equal(t, int64(2), hits.Load())
}

func TestRefreshOn401ReturnsIAMError(t *testing.T) {
	t.Parallel()

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	errIAM := errors.New("iam refused")

	_, err := do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays, Refresh: func(context.Context) (string, error) {
		return "", errIAM
	}}), http.MethodGet, url, nil)
	require.ErrorIs(t, err, errIAM)
}

// Отказ IAM при перевыпуске — не временный сбой: повтор GET снова пошёл бы в
// IAM с тем же ключом: 4 обращения и +17 с вместо одного.
func TestRefreshOn401IAMErrorNotRetried(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	})

	var refreshes atomic.Int64

	errIAM := errors.New("iam refused")
	rt := New(http.DefaultTransport, Options{RetryDelays: noDelays, Refresh: func(context.Context) (string, error) {
		refreshes.Add(1)
		return "", errIAM
	}})

	_, err := do(t, rt, http.MethodGet, url, nil)
	require.ErrorIs(t, err, errIAM)
	assert.Equal(t, int64(1), refreshes.Load())
	assert.Equal(t, int64(1), hits.Load())
}

func TestEmptyErrorBodyFromGrpcMessage(t *testing.T) {
	t.Parallel()

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Grpc-Message", "field%20name%20is%20required")
		w.WriteHeader(http.StatusBadRequest)
	})

	res, err := do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays}), http.MethodGet, url, nil)
	require.NoError(t, err)

	assert.JSONEq(t, `{"message":"field name is required"}`, res.body)
	assert.Equal(t, "application/json", res.header.Get("Content-Type"))
}

func TestEmptyErrorBodyFromHint(t *testing.T) {
	t.Parallel()

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })

	res, err := do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays, EmptyBodyHint: func(code int) string {
		return http.StatusText(code)
	}}), http.MethodGet, url, nil)
	require.NoError(t, err)

	assert.JSONEq(t, `{"message":"Not Found"}`, res.body)
}

func TestLimitBody(t *testing.T) {
	t.Parallel()

	url := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 4<<10))) })

	res, err := do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays, MaxBody: 1 << 10}), http.MethodGet, url, nil)
	require.NoError(t, err)

	require.ErrorContains(t, res.readErr, "exceeds")

	url1 := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 1<<10+1))) })

	res, err = do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays, MaxBody: 1 << 10}), http.MethodGet, url1, nil)
	require.NoError(t, err)
	require.ErrorContains(t, res.readErr, "exceeds", "на байт больше предела — уже ошибка")

	res, err = do(t, New(http.DefaultTransport, Options{RetryDelays: noDelays, MaxBody: 4 << 10}), http.MethodGet, url, nil)
	require.NoError(t, err)
	require.NoError(t, res.readErr)
	assert.Len(t, res.body, 4<<10, "ровно на пределе — читается")
}

func TestRetryGetOnTransportError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	down := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("connection refused")
	})

	_, err := do(t, New(down, Options{RetryDelays: noDelays}), http.MethodGet, "http://127.0.0.1:1", nil)
	require.ErrorContains(t, err, "connection refused")
	assert.Equal(t, int64(1+len(noDelays)), calls.Load())

	calls.Store(0)

	_, err = do(t, New(down, Options{RetryDelays: noDelays}), http.MethodPost, "http://127.0.0.1:1", strings.NewReader("{}"))
	require.Error(t, err)
	assert.Equal(t, int64(1), calls.Load(), "запись при обрыве сети не повторяется")
}
