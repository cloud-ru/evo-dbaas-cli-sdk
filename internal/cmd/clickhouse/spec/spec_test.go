package spec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	queries map[string]string
	calls   []string
}

func newFake(routes map[string]string) *fake {
	return &fake{routes: routes, queries: map[string]string{}}
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
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

func (f *fake) query(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.queries[key]
}

func (f *fake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

// run собирает команду с настоящим клиентом ClickHouse и буфером вывода.
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
	r, err := cloud.NewClickHouse(srv.URL, cloud.Options{
		HTTPClient: srv.Client(),
		Auth:       func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	var out, errOut bytes.Buffer

	s := &state.State{
		Printer: &output.Printer{Out: &out, Err: &errOut, JSON: json}, Project: "p1",
		Timeout: time.Second, PollDelay: time.Millisecond,
		Env:        func(string) string { return "" },
		ClickHouse: func() (*cloud.ClickHouse, error) { return r, nil },
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

func TestListRequiresZoneAndValidRoleBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"list"}, "--zone is required"},
		{[]string{"list", "--zone", "z1", "--role", "bad"}, "--role"},
		{[]string{"list", "--zone", "z1", "--role", "replica"}, `--role "replica": allowed values: server | keeper`},
	} {
		f := newFake(nil)
		_, err := run(t, f, false, tc.args...)
		require.ErrorContains(t, err, tc.want)
		assert.Equal(t, 1, output.ExitCode(err))
		assert.Empty(t, f.requests())
	}
}

func TestListSendsFiltersAndPrintsRanges(t *testing.T) {
	t.Parallel()

	raw := `{"specifications":[{"id":"s1","name":"small","cpu":2,"memory":4.5,"cluster_type":"SINGLE","min_shards":1,"max_shards":3,"min_replicas":1,"max_replicas":3,"min_storage_gigs":40,"role":"SPECIFICATION_ROLE_CLICKHOUSE_SERVER","future":17},{"id":"s2","name":"empty"}]}`
	for _, role := range []string{"server", "keeper", ""} {
		for _, json := range []bool{false, true} {
			f := newFake(map[string]string{"GET /v1/specifications": raw})
			out, err := run(t, f, json, "list", "--version", "25.8", "--zone", "z1", "--role", role)
			require.NoError(t, err)
			q, err := url.ParseQuery(f.query("GET /v1/specifications"))
			require.NoError(t, err)

			want := url.Values{"version": {"25.8"}, "availability_zone_id": {"z1"}}
			if role != "" {
				want.Set("role", "SPECIFICATION_ROLE_CLICKHOUSE_"+strings.ToUpper(role))
			}

			assert.Equal(t, want, q)
			assert.Equal(t, []string{"GET /v1/specifications"}, f.requests())

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				require.Len(t, lines, 3)
				assert.Equal(t, strings.Fields("ID NAME CPU RAM_GB TYPE SHARDS REPLICAS MIN_STORAGE ROLE"), strings.Fields(lines[0]))
				assert.Equal(t, strings.Fields("s1 small 2 4.5 SINGLE 1–3 1–3 40 CLICKHOUSE_SERVER"), strings.Fields(lines[1]))
				assert.Equal(t, strings.Fields("s2 empty – –"), strings.Fields(lines[2]))
			}
		}
	}
}

func TestListDefaultsToServerWithoutVersion(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"GET /v1/specifications": `{}`})
	_, err := run(t, f, false, "list", "--zone", "z1")
	require.NoError(t, err)
	q, err := url.ParseQuery(f.query("GET /v1/specifications"))
	require.NoError(t, err)
	assert.Equal(t, url.Values{"availability_zone_id": {"z1"}, "role": {"SPECIFICATION_ROLE_CLICKHOUSE_SERVER"}}, q)
}

func TestListResolvesZoneNameThroughCompute(t *testing.T) {
	t.Parallel()

	zones := newFake(map[string]string{"GET /api/v1/availability-zones": `[{"id":"z1","name":"ru.AZ-1"}]`})
	srv := httptest.NewServer(zones)
	t.Cleanup(srv.Close)
	compute, err := cloud.NewCompute(srv.URL, cloud.Options{
		HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	f := newFake(map[string]string{"GET /v1/specifications": `{"specifications":[]}`})
	_, _, err = runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
		s.Compute = func() (*cloud.Compute, error) { return compute, nil }
	}, "list", "--version", "v7", "--zone", "RU.az-1")
	require.NoError(t, err)
	q, err := url.ParseQuery(f.query("GET /v1/specifications"))
	require.NoError(t, err)
	assert.Equal(t, "z1", q.Get("availability_zone_id"))
	assert.Equal(t, []string{"GET /api/v1/availability-zones"}, zones.requests())
}

func TestListPropagatesClientErrorsBeforeRequest(t *testing.T) {
	t.Parallel()

	want := errors.New("клиент не настроен")
	for _, service := range []string{"ClickHouse", "Compute"} {
		t.Run(service, func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)

			zone := "z1"
			if service == "Compute" {
				zone = "ru.AZ-1"
			}

			_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
				// Сбой только у своего клиента: ошибка зоны не должна уйти в запрос спецификаций.
				if service == "Compute" {
					s.Compute = func() (*cloud.Compute, error) { return nil, want }
				} else {
					s.ClickHouse = func() (*cloud.ClickHouse, error) { return nil, want }
				}
			}, "list", "--version", "v7", "--zone", zone)
			require.ErrorIs(t, err, want)
			assert.Empty(t, f.requests())
		})
	}
}

func TestListReportsRequestAndDecodeErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		routes map[string]string
		want   string
	}{
		{"ошибка API", nil, "API 404"},
		{"неверное число", map[string]string{"GET /v1/specifications": `{"specifications":[{"cpu":"много"}]}`}, "parse response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := run(t, newFake(tc.routes), false, "list", "--zone", "z1")
			require.ErrorContains(t, err, tc.want)
		})
	}
}
