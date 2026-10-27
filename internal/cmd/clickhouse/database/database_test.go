package database

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
	mu     sync.Mutex
	routes map[string]string
	bodies map[string]string
	calls  []string
}

func newFake(routes map[string]string) *fake {
	return &fake{routes: routes, bodies: map[string]string{}}
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.bodies[key] = string(b)
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

func TestListAndGetPrintDatabaseAndRawJSON(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"list", "get"} {
		for _, json := range []bool{false, true} {
			route, raw := "GET /v1/clusters/c1/databases", `{"databases":[{"name":"analytics","engine":"Atomic","future":17}]}`

			args := []string{verb, "c1"}
			if verb == "get" {
				route += "/analytics"
				raw = `{"name":"analytics","engine":"Atomic","future":17}`

				args = append(args, "analytics")
			}

			f := newFake(map[string]string{route: raw})
			out, err := run(t, f, json, args...)
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
			} else if verb == "get" {
				assert.Equal(t, "analytics\tAtomic\n", out)
			} else {
				assert.Equal(t, []string{"NAME", "ENGINE", "analytics", "Atomic"}, strings.Fields(out))
			}

			assert.Equal(t, []string{route}, f.requests())
			assert.Empty(t, f.body(route))
		}
	}
}

func TestMutationsPrintResponseAndMessageWithoutPolling(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"create", "delete"} {
		for _, json := range []bool{false, true} {
			route := "POST /v1/clusters/c1/databases"
			args := []string{verb, "c1", "analytics"}

			message := "database analytics created\n"
			if verb == "delete" {
				route = "DELETE /v1/clusters/c1/databases/analytics"

				args = append(args, "--yes")
				message = "database analytics deleted\n"
			}

			raw := `{"name":"analytics","future":17}`
			f := newFake(map[string]string{route: raw})
			out, stderr, err := runWith(t, f, json, nil, args...)
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
				assert.Empty(t, stderr)
			} else {
				assert.Empty(t, out)
				assert.Equal(t, message, stderr)
			}

			assert.Equal(t, []string{route}, f.requests())

			if verb == "create" {
				assert.JSONEq(t, `{"cluster_id":"c1","name":"analytics"}`, f.body(route))
			} else {
				assert.Empty(t, f.body(route))
			}
		}
	}
}

func TestDeleteRequiresConfirmationBeforeRequest(t *testing.T) {
	t.Parallel()

	f := newFake(nil)
	_, err := run(t, f, false, "delete", "c1", "analytics")
	require.ErrorContains(t, err, "--yes")
	assert.Equal(t, 1, output.ExitCode(err))
	assert.Empty(t, f.requests())
}

func TestCommandsPropagateClientAndAPIErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args  []string
		route string
	}{
		{[]string{"list", "c1"}, "GET /v1/clusters/c1/databases"},
		{[]string{"get", "c1", "db"}, "GET /v1/clusters/c1/databases/db"},
		{[]string{"create", "c1", "db"}, "POST /v1/clusters/c1/databases"},
		{[]string{"delete", "c1", "db", "--yes"}, "DELETE /v1/clusters/c1/databases/db"},
	} {
		for _, initError := range []bool{false, true} {
			want := errors.New("client unavailable")
			f := newFake(nil)

			out, stderr, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
				if initError {
					s.ClickHouse = func() (*cloud.ClickHouse, error) { return nil, want }
				}
			}, tc.args...)
			if initError {
				require.ErrorIs(t, err, want)
				assert.Empty(t, f.requests())
			} else {
				var ae *cloud.APIError
				require.ErrorAs(t, err, &ae)
				assert.Equal(t, 404, ae.Status)
				assert.Equal(t, []string{tc.route}, f.requests())
			}

			assert.Empty(t, out)
			assert.Empty(t, stderr)
			assert.Equal(t, 1, output.ExitCode(err))
		}
	}
}

func TestListAndGetReportDecodeErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args       []string
		route, raw string
	}{
		{[]string{"list", "c1"}, "GET /v1/clusters/c1/databases", `{"databases":42}`},
		{[]string{"get", "c1", "db"}, "GET /v1/clusters/c1/databases/db", `{"engine":42}`},
	} {
		f := newFake(map[string]string{tc.route: tc.raw})
		_, err := run(t, f, false, tc.args...)
		require.ErrorContains(t, err, "parse response")
		assert.Equal(t, []string{tc.route}, f.requests())
	}
}
