package user

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
	mu       sync.Mutex
	routes   map[string]string
	statuses map[string]int
	queries  map[string]string
	bodies   map[string]string
	calls    []string
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
	status := f.statuses[key]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"no route"}`)

		return
	}

	if status != 0 {
		w.WriteHeader(status)
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

func (f *fake) query(key string) string { f.mu.Lock(); defer f.mu.Unlock(); return f.queries[key] }

func TestMutationsPrintDirectResponseAndHumanMessage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                        string
		args                        []string
		route, body, message, query string
	}{
		{"create env", []string{"create", "c1", "app", "--password-env", "PW", "--role", "reader,writer"}, "POST /v1/clusters/c1/users", `{"cluster_id":"c1","user_name":"app","password":"secret","granted_roles":{"value":["reader","writer"]}}`, "user app created", ""},
		{"create stdin", []string{"create", "c1", "app", "--password-stdin"}, "POST /v1/clusters/c1/users", `{"cluster_id":"c1","user_name":"app","password":"secret"}`, "user app created", ""},
		{"update password", []string{"update", "c1", "app", "--password-env", "PW"}, "PATCH /v1/clusters/c1/users/app", `{"cluster_id":"c1","user_name":"app","password":"secret"}`, "user app updated", ""},
		{"update roles", []string{"update", "c1", "app", "--role", "reader,writer"}, "PATCH /v1/clusters/c1/users/app", `{"cluster_id":"c1","user_name":"app","granted_roles":{"value":["reader","writer"]}}`, "user app updated", ""},
		{"delete", []string{"delete", "c1", "app", "--yes"}, "DELETE /v1/clusters/c1/users/app", "", "user app deleted", ""},
		{"grant", []string{"grant", "c1", "app", "reader"}, "POST /v1/clusters/c1/users/app:grantRole", "", "grant: reader → app", "role=reader"},
		{"revoke", []string{"revoke", "c1", "app", "reader"}, "POST /v1/clusters/c1/users/app:revokeRole", "", "revoke: reader → app", "role=reader"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, json := range []bool{false, true} {
				const raw = `{"future":{"success":true}}`

				f := newFake(map[string]string{tc.route: raw})
				out, stderr, err := runWith(t, f, json, func(s *state.State, cmd *cobra.Command) {
					s.Env = func(name string) string { assert.Equal(t, "PW", name); return "secret" }

					cmd.SetIn(strings.NewReader("secret\r\nignored\n"))
				}, tc.args...)
				require.NoError(t, err)
				assert.Equal(t, []string{tc.route}, f.requests())
				assert.Equal(t, tc.query, f.query(tc.route))

				if tc.body != "" {
					assert.JSONEq(t, tc.body, f.body(tc.route))
				} else {
					assert.Empty(t, f.body(tc.route))
				}

				if json {
					assert.JSONEq(t, raw, out)
					assert.Empty(t, stderr)
				} else {
					assert.Empty(t, out)
					assert.Equal(t, tc.message+"\n", stderr)
				}
			}
		})
	}
}

func TestUpdateReadsPasswordBeforeValidatingChanges(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"env", "stdin"} {
		for _, empty := range []bool{false, true} {
			f := newFake(nil)
			reads := 0

			args := []string{"update", "c1", "app", "--role", "reader"}
			if source == "env" {
				args = append(args, "--password-env", "PW")
			} else {
				args = append(args, "--password-stdin")
			}

			_, _, err := runWith(t, f, false, func(s *state.State, cmd *cobra.Command) {
				s.Env = func(string) string {
					reads++

					if empty {
						return ""
					}

					return "secret"
				}

				value := "secret\n"
				if empty {
					value = "\n"
				}

				cmd.SetIn(io.TeeReader(strings.NewReader(value), countWriter{&reads}))
			}, args...)
			if empty {
				require.ErrorContains(t, err, "a password is required")
			} else {
				require.ErrorContains(t, err, "ClickHouse passwords and roles must be changed in separate calls")
			}

			assert.Positive(t, reads)
			assert.Equal(t, 1, output.ExitCode(err))
			assert.Empty(t, f.requests())
		}
	}
}

// countWriter считает чтения stdin: TeeReader пишет в него каждый прочитанный кусок.
type countWriter struct{ n *int }

func (w countWriter) Write(b []byte) (int, error) { *w.n++; return len(b), nil }

func TestValidationBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"create", "c1", "app"}, "a password is required"},
		{[]string{"update", "c1", "app"}, "specify what to change"},
		{[]string{"delete", "c1", "app"}, "--yes"},
		{[]string{"update", "c1", "app", "--password-env", "PW", "--password-stdin"}, "cannot be used together"},
	} {
		f := newFake(nil)
		_, err := run(t, f, false, tc.args...)
		require.ErrorContains(t, err, tc.want)
		assert.Equal(t, 1, output.ExitCode(err))
		assert.Empty(t, f.requests())
	}
}

func TestGetAndListPrintRolesAndPreserveJSON(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw, roles string }{
		{"new roles", `{"name":"app","granted_roles":{"value":["reader","writer"]},"roles":["obsolete"],"read_only":true,"future":17}`, "reader,writer"},
		{"legacy roles", `{"name":"app","roles":["reader","writer"]}`, "reader,writer"},
		{"empty roles", `{"name":"app","granted_roles":{"value":[]},"roles":["obsolete"]}`, ""},
		{"empty wrapper", `{"name":"app","granted_roles":{},"roles":["reader"]}`, "reader"},
		{"missing roles", `{"name":"app"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, verb := range []string{"get", "list"} {
				for _, json := range []bool{false, true} {
					route, raw := "GET /v1/clusters/c1/users", `{"users":[`+tc.raw+`]}`

					args := []string{verb, "c1"}
					if verb == "get" {
						route += "/app"
						raw = tc.raw

						args = append(args, "app")
					}

					f := newFake(map[string]string{route: raw})
					out, err := run(t, f, json, args...)
					require.NoError(t, err)
					assert.Equal(t, []string{route}, f.requests())

					if json {
						assert.JSONEq(t, raw, out)
					} else if verb == "get" {
						assert.Equal(t, "app\t"+tc.roles+"\n", out)
					} else {
						lines := strings.Split(strings.TrimSpace(out), "\n")
						require.Len(t, lines, 2)
						assert.Equal(t, []string{"NAME", "ROLES", "READ_ONLY"}, strings.Fields(lines[0]))

						readonly := "false"
						if tc.name == "new roles" {
							readonly = "true"
						}

						assert.Equal(t, strings.Fields("app "+tc.roles+" "+readonly), strings.Fields(lines[1]))
					}
				}
			}
		})
	}
}

func TestCommandsPropagateClientAndAPIErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args  []string
		route string
	}{
		{[]string{"create", "c1", "app", "--password-env", "PW"}, "POST /v1/clusters/c1/users"},
		{[]string{"update", "c1", "app", "--role", "reader"}, "PATCH /v1/clusters/c1/users/app"},
		{[]string{"delete", "c1", "app", "--yes"}, "DELETE /v1/clusters/c1/users/app"},
		{[]string{"grant", "c1", "app", "reader"}, "POST /v1/clusters/c1/users/app:grantRole"},
		{[]string{"revoke", "c1", "app", "reader"}, "POST /v1/clusters/c1/users/app:revokeRole"},
		{[]string{"get", "c1", "app"}, "GET /v1/clusters/c1/users/app"},
		{[]string{"list", "c1"}, "GET /v1/clusters/c1/users"},
	} {
		for _, initError := range []bool{false, true} {
			want := errors.New("client unavailable")
			f := newFake(nil)

			out, stderr, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
				s.Env = func(string) string { return "secret" }
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

func TestGetAndListRejectMalformedResponse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args       []string
		route, raw string
	}{
		{[]string{"get", "c1", "app"}, "GET /v1/clusters/c1/users/app", `{"roles":42}`},
		{[]string{"list", "c1"}, "GET /v1/clusters/c1/users", `{"users":[{"read_only":"bad"}]}`},
	} {
		f := newFake(map[string]string{tc.route: tc.raw})
		_, err := run(t, f, false, tc.args...)
		require.ErrorContains(t, err, "parse response")
		assert.Equal(t, []string{tc.route}, f.requests())
	}
}
