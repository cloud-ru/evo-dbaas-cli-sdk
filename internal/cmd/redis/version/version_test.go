package version

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestListPrintsAvailableValues(t *testing.T) {
	t.Parallel()

	raw := `{"versions":[{"id":"v7","name":"Redis 7","future":{"x":1}}]}`
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "таблица", true: "JSON без потери полей"}[json], func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"GET /v1/versions": raw})
			out, err := run(t, f, json, "list")
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Contains(t, out, "ID")
				assert.Contains(t, out, "NAME")
				assert.Contains(t, out, "v7")
				assert.Contains(t, out, "Redis 7")
			}

			assert.Equal(t, []string{"GET /v1/versions"}, f.requests())
		})
	}
}

func TestListReportsClientError(t *testing.T) {
	t.Parallel()

	want := errors.New("Redis недоступен: нет ключа")
	f := newFake(nil)
	out, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
		s.Redis = func() (*cloud.Redis, error) { return nil, want }
	}, "list")
	require.ErrorIs(t, err, want)
	assert.Contains(t, err.Error(), want.Error())
	assert.Empty(t, out)
	assert.Empty(t, f.requests())
}

func TestListReportsRequestAndDecodeErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		routes map[string]string
		want   string
	}{
		{"API недоступен", nil, "API 404"},
		{"неверная схема", map[string]string{"GET /v1/versions": `{"versions":"не список"}`}, "parse response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := run(t, newFake(tc.routes), false, "list")
			require.ErrorContains(t, err, tc.want)
			assert.Empty(t, out)
		})
	}
}
