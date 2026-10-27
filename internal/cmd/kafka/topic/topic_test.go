package topic

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

func (f *fake) body(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bodies[key]
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

// run собирает команду с настоящим клиентом Kafka и буфером вывода.
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
	r, err := cloud.NewKafka(srv.URL, cloud.Options{
		HTTPClient: srv.Client(),
		Auth:       func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	var out, errOut bytes.Buffer

	s := &state.State{
		Printer: &output.Printer{Out: &out, Err: &errOut, JSON: json}, Project: "p1",
		Timeout: time.Second, PollDelay: time.Millisecond,
		Env:   func(string) string { return "" },
		Kafka: func() (*cloud.Kafka, error) { return r, nil },
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

func TestListSendsFilterAndPrintsTableOrJSON(t *testing.T) {
	t.Parallel()

	raw := `{"topics":[{"name":"events","num_partitions":3,"replication_factor":2,"future":17}]}`
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "таблица", true: "JSON"}[json], func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"GET /v1/clusters/c1/topics": raw})
			out, err := run(t, f, json, "list", "c1", "--filter", "event")
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Equal(t, []string{"NAME", "PARTITIONS", "REPLICATION", "events", "3", "2"}, strings.Fields(out))
			}

			q, err := url.ParseQuery(f.query("GET /v1/clusters/c1/topics"))
			require.NoError(t, err)
			assert.Equal(t, url.Values{"page_size": {"100"}, "filter_by_partial_topic_name": {"event"}}, q)
		})
	}
}

func TestGetSortsParametersAndPreservesJSON(t *testing.T) {
	t.Parallel()

	raw := `{"name":"events","num_partitions":3,"replication_factor":2,"parameters":{"z":"last","a":"first","m":"middle"},"future":17}`
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "параметры по алфавиту", true: "JSON"}[json], func(t *testing.T) {
			t.Parallel()

			out, err := run(t, newFake(map[string]string{"GET /v1/clusters/c1/topics/events": raw}), json, "get", "c1", "events")
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Equal(t, "events\tpartitions=3\treplication=2\n  a=first\n  m=middle\n  z=last\n", out)
			}
		})
	}
}

func TestCreateSendsParametersAndPrintsOperation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"параметры", []string{"--partitions", "3", "--replication", "2", "--param", "retention.ms=1000", "--param", "cleanup.policy=compact"}, `{"cluster_id":"c1","name":"events","num_partitions":3,"replication_factor":2,"parameters":{"retention.ms":"1000","cleanup.policy":"compact"}}`},
		{"по умолчанию", nil, `{"cluster_id":"c1","name":"events","num_partitions":1,"replication_factor":1,"parameters":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"POST /v1/clusters/c1/topics": `{"id":"op1","done":false}`})
			out, err := run(t, f, false, append([]string{"create", "c1", "events"}, tc.args...)...)
			require.NoError(t, err)
			assert.Contains(t, out, "op1")
			assert.JSONEq(t, tc.want, f.body("POST /v1/clusters/c1/topics"))
			assert.Equal(t, []string{"POST /v1/clusters/c1/topics"}, f.requests())
		})
	}
}

func TestUpdateSendsChangedFields(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"PATCH /v1/clusters/c1/topics/events": `{"id":"op1","done":false}`})
	out, err := run(t, f, false, "update", "c1", "events", "--partitions", "5", "--replication", "3", "--param", "retention.ms=2000")
	require.NoError(t, err)
	assert.Contains(t, out, "op1")
	assert.JSONEq(t, `{"cluster_id":"c1","name":"events","num_partitions":5,"replication_factor":3,"parameters":{"retention.ms":"2000"}}`, f.body("PATCH /v1/clusters/c1/topics/events"))
}

func TestChangesRejectInvalidFlagsBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"create параметр", []string{"create", "c1", "events", "--param", "bad"}, "key=value"},
		{"update параметр", []string{"update", "c1", "events", "--param", "bad"}, "key=value"},
		{"update без изменений", []string{"update", "c1", "events"}, "specify what to change"},
		{"delete без подтверждения", []string{"delete", "c1", "events"}, "--yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			_, err := run(t, f, false, tc.args...)
			require.ErrorContains(t, err, tc.want)
			assert.Empty(t, f.requests())
		})
	}
}

func TestMutationsWaitForOperation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, method, path string
		flags              []string
	}{
		{"create", "POST", "/v1/clusters/c1/topics", nil},
		{"update", "PATCH", "/v1/clusters/c1/topics/events", []string{"--partitions", "4"}},
		{"delete", "DELETE", "/v1/clusters/c1/topics/events", []string{"--yes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key := tc.method + " " + tc.path
			f := newFake(map[string]string{
				key:                      `{"id":"op1","done":false}`,
				"GET /v1/operations/op1": `{"id":"op1","done":true}`,
			})

			args := append([]string{tc.name, "c1", "events", "--wait"}, tc.flags...)
			out, err := run(t, f, false, args...)
			require.NoError(t, err)
			assert.Contains(t, out, "operation op1 — completed")
			assert.Equal(t, []string{key, "GET /v1/operations/op1"}, f.requests())
		})
	}
}

func TestCommandsPropagateClientErrorsBeforeRequest(t *testing.T) {
	t.Parallel()

	want := errors.New("Kafka недоступен")
	for _, args := range [][]string{
		{"list", "c1"},
		{"get", "c1", "events"},
		{"create", "c1", "events"},
		{"update", "c1", "events", "--partitions", "4"},
		{"delete", "c1", "events", "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
				s.Kafka = func() (*cloud.Kafka, error) { return nil, want }
			}, args...)
			require.ErrorIs(t, err, want)
			assert.Empty(t, f.requests())
		})
	}
}

func TestCommandsReportAPIAndDecodeErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		args   []string
		routes map[string]string
		want   string
	}{
		{"list API", []string{"list", "c1"}, nil, "API 404"},
		{"get API", []string{"get", "c1", "events"}, nil, "API 404"},
		{"create API", []string{"create", "c1", "events"}, nil, "API 404"},
		{"update API", []string{"update", "c1", "events", "--partitions", "4"}, nil, "API 404"},
		{"delete API", []string{"delete", "c1", "events", "--yes"}, nil, "API 404"},
		{"list схема", []string{"list", "c1"}, map[string]string{"GET /v1/clusters/c1/topics": `{"topics":[{"num_partitions":"bad"}]}`}, "parse response"},
		{"get схема", []string{"get", "c1", "events"}, map[string]string{"GET /v1/clusters/c1/topics/events": `{"parameters":42}`}, "parse response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := run(t, newFake(tc.routes), false, tc.args...)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
