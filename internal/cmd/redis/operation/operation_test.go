package operation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// fake — API: ответ по «МЕТОД /путь», запросы сохраняются для проверок.
type fake struct {
	mu      sync.Mutex
	routes  map[string]string
	bodies  map[string]string
	queries map[string]string
	calls   []string
}

func newFake(routes map[string]string) *fake {
	return &fake{routes: routes, bodies: map[string]string{}, queries: map[string]string{}}
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.bodies[key] = string(b)
	f.queries[key] = r.URL.RawQuery
	f.calls = append(f.calls, key)
	body, ok := f.routes[key]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"no route"}`)

		return
	}

	_, _ = io.WriteString(w, body)
}

func (f *fake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

// run собирает команду с настоящим клиентом Redis и буфером вывода.
func run(t *testing.T, f *fake, json bool, args ...string) (string, error) {
	t.Helper()
	out, _, err := runWith(t, f, json, nil, args...)

	return out, err
}

// runWith позволяет задать окружение, stdin или ошибку создания клиента.
func runWith(t *testing.T, f *fake, json bool, setup func(*state.State, *cobra.Command), args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	r, err := cloud.NewRedis(srv.URL, cloud.Options{
		HTTPClient: srv.Client(),
		Auth:       func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	var out, errOut bytes.Buffer

	s := &state.State{
		Printer: &output.Printer{Out: &out, Err: &errOut, JSON: json}, Project: "p1",
		Timeout: time.Second, PollDelay: time.Millisecond,
		Env:   func(string) string { return "" },
		Redis: func() (*cloud.Redis, error) { return r, nil },
	}

	cmd := NewCmd(s)
	if setup != nil {
		setup(s, cmd)
	}

	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err = cmd.ExecuteContext(t.Context())

	return out.String(), errOut.String(), err
}

func TestGetWithoutWaitPrintsOperationOnce(t *testing.T) {
	t.Parallel()

	raw := `{"id":"op1","done":false,"description":"создание","resource_id":"c1","future":42}`
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "статус", true: "JSON"}[json], func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"GET /v1/operations/op1": raw})
			out, err := run(t, f, json, "get", "op1")
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Contains(t, out, "operation op1 — in progress")
				assert.Contains(t, out, "resource  c1")
				assert.Contains(t, out, "создание")
			}

			assert.Equal(t, []string{"GET /v1/operations/op1"}, f.requests())
		})
	}
}

func TestGetNotFoundDoesNotStartWaiting(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"get", "missing"}, {"get", "missing", "--wait"}} {
		t.Run(args[len(args)-1], func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			out, err := run(t, f, false, args...)
			require.ErrorContains(t, err, "API 404")
			assert.Equal(t, 1, output.ExitCode(err))
			assert.Empty(t, out)
			assert.Equal(t, []string{"GET /v1/operations/missing"}, f.requests())
		})
	}
}

func TestGetWaitPrintsCompletedOperation(t *testing.T) {
	t.Parallel()
	// Завершённая операция не требует повторного GET.
	f := newFake(map[string]string{"GET /v1/operations/op1": `{"id":"op1","done":true}`})
	out, err := run(t, f, false, "get", "op1", "--wait")
	require.NoError(t, err)
	assert.Contains(t, out, "operation op1 — completed")
	assert.Len(t, f.requests(), 1)
}

func TestListPrintsTableAndRawJSON(t *testing.T) {
	t.Parallel()

	raw := `{"operations":[{"id":"op1","done":true,"created_at":"2026-10-26T12:00:00Z","description":"готово","future":17},{"id":"op2","done":false}]}`
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "таблица", true: "JSON"}[json], func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"GET /v1/clusters/c1/operations": raw})
			out, err := run(t, f, json, "list", "c1")
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				for _, want := range []string{"ID", "DONE", "CREATED", "DESCRIPTION", "op1", "op2", "true", "false", "2026-10-26T12:00:00Z", "готово"} {
					assert.Contains(t, out, want)
				}

				// DONE — у своей строки: перестановка true/false иначе не видна.
				rows := map[string][]string{}
				for _, line := range strings.Split(out, "\n") {
					if cols := strings.Fields(line); len(cols) > 1 {
						rows[cols[0]] = cols
					}
				}

				assert.Equal(t, "true", rows["op1"][1])
				assert.Equal(t, "false", rows["op2"][1])
			}
		})
	}
}

func TestClientErrorsReachGetListAndWait(t *testing.T) {
	t.Parallel()

	want := errors.New("Redis: не задан ключ")
	for _, args := range [][]string{{"get", "op1"}, {"list", "c1"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
				s.Redis = func() (*cloud.Redis, error) { return nil, want }
			}, args...)
			require.ErrorIs(t, err, want)
			assert.Empty(t, f.requests())
		})
	}

	_, err := Ops(&state.State{Redis: func() (*cloud.Redis, error) { return nil, want }})
	require.ErrorIs(t, err, want)
}

func TestListReportsAPIErrorsAndMalformedOperations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		routes map[string]string
		want   string
	}{
		{"ошибка запроса", nil, "API 404"},
		{"неверная дата", map[string]string{"GET /v1/clusters/c1/operations": `{"operations":[{"id":"op1","created_at":"вчера"}]}`}, "parse response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := run(t, newFake(tc.routes), false, "list", "c1")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// get --wait доходит до ожидания: операция, завершённая ошибкой, — код 1 и
// причина; без ожидания та же операция печаталась бы с кодом 0.
func TestGetWaitReportsFailedOperation(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"GET /v1/operations/op1": `{"id":"op1","done":true,"error":{"message":"нет квоты"}}`})
	out, err := run(t, f, false, "get", "op1", "--wait")
	require.ErrorContains(t, err, "нет квоты")
	assert.Equal(t, 1, output.ExitCode(err))
	assert.Contains(t, out, "failed: нет квоты")
}
