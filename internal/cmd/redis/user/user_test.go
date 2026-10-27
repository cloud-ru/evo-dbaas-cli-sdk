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

func TestCreateReadsPasswordAndSendsGrantedRoles(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"env", "stdin"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"POST /v1/clusters/c1/users": `{"id":"op1","done":false}`})

			args := []string{"create", "c1", "app", "--role", "reader", "--role", "writer"}
			if source == "env" {
				args = append(args, "--password-env", "REDIS_PASSWORD")
			} else {
				args = append(args, "--password-stdin")
			}

			out, _, err := runWith(t, f, false, func(s *state.State, cmd *cobra.Command) {
				s.Env = func(name string) string {
					assert.Equal(t, "REDIS_PASSWORD", name)
					return "secret"
				}

				cmd.SetIn(strings.NewReader("secret\r\nвторая строка\n"))
			}, args...)
			require.NoError(t, err)
			assert.Contains(t, out, "operation op1 — in progress")
			assert.JSONEq(t, `{"cluster_id":"c1","user_name":"app","password":"secret","granted_roles":{"value":["reader","writer"]}}`, f.body("POST /v1/clusters/c1/users"))
			assert.Equal(t, []string{"POST /v1/clusters/c1/users"}, f.requests())
		})
	}
}

func TestCreateWaitsForOperation(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{
		"POST /v1/clusters/c1/users": `{"id":"op1","done":false}`,
		"GET /v1/operations/op1":     `{"id":"op1","done":true}`,
	})
	out, _, err := runWith(t, f, false, func(_ *state.State, cmd *cobra.Command) {
		cmd.SetIn(strings.NewReader("secret\n"))
	}, "create", "c1", "app", "--password-stdin", "--wait")
	require.NoError(t, err)
	assert.Contains(t, out, "operation op1 — completed")
	assert.JSONEq(t, `{"cluster_id":"c1","user_name":"app","password":"secret"}`, f.body("POST /v1/clusters/c1/users"))
	assert.Equal(t, []string{"POST /v1/clusters/c1/users", "GET /v1/operations/op1"}, f.requests())
}

func TestUpdateRequiresChangesBeforeRequest(t *testing.T) {
	t.Parallel()

	f := newFake(nil)
	_, err := run(t, f, false, "update", "c1", "app")
	require.ErrorContains(t, err, "specify what to change")
	assert.Equal(t, 1, output.ExitCode(err))
	assert.Empty(t, f.requests())
}

func TestUpdatePasswordPreservesCurrentRoles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		current string
	}{
		{"granted_roles предпочтительнее старого поля", `{"name":"app","granted_roles":{"value":["reader","writer"]},"roles":["obsolete"]}`},
		{"старое поле roles", `{"name":"app","roles":["reader","writer"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{
				"GET /v1/clusters/c1/users/app": tc.current,
				"PUT /v1/clusters/c1/users/app": `{"id":"op1","done":false}`,
			})
			out, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
				s.Env = func(name string) string {
					assert.Equal(t, "NEW_PASSWORD", name)
					return "new-secret"
				}
			}, "update", "c1", "app", "--password-env", "NEW_PASSWORD")
			require.NoError(t, err)
			assert.Contains(t, out, "op1")
			assert.JSONEq(t, `{"cluster_id":"c1","user_name":"app","password":"new-secret","granted_roles":{"value":["reader","writer"]}}`, f.body("PUT /v1/clusters/c1/users/app"))
			assert.Equal(t, []string{"GET /v1/clusters/c1/users/app", "PUT /v1/clusters/c1/users/app"}, f.requests())
		})
	}
}

func TestUpdatePasswordRefusesOnEmptyUserResponse(t *testing.T) {
	t.Parallel()

	// Как в старом CLI: пустое тело GET — ошибка разбора, PUT без ролей не уходит.
	f := newFake(map[string]string{"GET /v1/clusters/c1/users/app": ""})
	_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
		s.Env = func(string) string { return "new-secret" }
	}, "update", "c1", "app", "--password-env", "NEW_PASSWORD")
	require.ErrorContains(t, err, "parse response")
	assert.Equal(t, []string{"GET /v1/clusters/c1/users/app"}, f.requests())
}

func TestUpdateExplicitRolesSkipsGetAndLeavesPasswordUnchanged(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"PUT /v1/clusters/c1/users/app": `{"id":"op1","done":false}`})
	_, err := run(t, f, false, "update", "c1", "app", "--role", "reader,writer")
	require.NoError(t, err)
	assert.JSONEq(t, `{"cluster_id":"c1","user_name":"app","granted_roles":{"value":["reader","writer"]}}`, f.body("PUT /v1/clusters/c1/users/app"))
	assert.Equal(t, []string{"PUT /v1/clusters/c1/users/app"}, f.requests())
}

func TestDeleteRequiresConfirmationAndHasNoWaitFlag(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"DELETE /v1/clusters/c1/users/app": `{}`})
	_, err := run(t, f, false, "delete", "c1", "app")
	require.ErrorContains(t, err, "--yes")
	assert.Empty(t, f.requests())

	_, err = run(t, f, false, "delete", "c1", "app", "--yes", "--wait")
	require.ErrorContains(t, err, "unknown flag: --wait")
	assert.Empty(t, f.requests())

	out, err := run(t, f, true, "delete", "c1", "app", "--yes")
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, out)
	assert.Equal(t, []string{"DELETE /v1/clusters/c1/users/app"}, f.requests())
}

func TestGetAndListPrintRolesSeparatedByCommas(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"новые роли", `{"name":"app","granted_roles":{"value":["reader","writer"]},"roles":["obsolete"],"future":17}`, "reader,writer"},
		{"старые роли", `{"name":"app","roles":["reader","writer"]}`, "reader,writer"},
		{"нет ролей", `{"name":"app"}`, ""},
		{"пустые новые роли не подменяются старыми", `{"name":"app","granted_roles":{"value":[]},"roles":["obsolete"]}`, ""},
		{"пустая обёртка допускает старое поле", `{"name":"app","granted_roles":{},"roles":["reader","writer"]}`, "reader,writer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, verb := range []string{"get", "list"} {
				for _, json := range []bool{false, true} {
					path, raw := "/v1/clusters/c1/users", `{"users":[`+tc.raw+`]}`

					args := []string{verb, "c1"}
					if verb == "get" {
						path, raw = path+"/app", tc.raw

						args = append(args, "app")
					}

					out, err := run(t, newFake(map[string]string{"GET " + path: raw}), json, args...)
					require.NoError(t, err)

					if json {
						assert.JSONEq(t, raw, out)
					} else {
						assert.Contains(t, out, "app")
						assert.Contains(t, out, tc.want)
						assert.NotContains(t, out, "obsolete")

						if verb == "list" {
							assert.Contains(t, out, "NAME")
							assert.Contains(t, out, "ROLES")
						} else {
							assert.Equal(t, "app\t"+tc.want+"\n", out)
						}
					}
				}
			}
		})
	}
}

func TestCommandsPropagateRedisInitializationErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("Redis: нет доступа к конфигурации")
	for _, args := range [][]string{
		{"create", "c1", "app", "--password-env", "PW"},
		{"update", "c1", "app", "--role", "reader"},
		{"delete", "c1", "app", "--yes"},
		{"get", "c1", "app"},
		{"list", "c1"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
				s.Env = func(string) string { return "secret" }
				s.Redis = func() (*cloud.Redis, error) { return nil, want }
			}, args...)
			require.ErrorIs(t, err, want)
			assert.Contains(t, err.Error(), want.Error())
			assert.Empty(t, f.requests())
		})
	}
}

func TestCommandsReportPasswordRequestAndDecodeErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		args     []string
		routes   map[string]string
		want     string
		requests []string
	}{
		{"create без пароля", []string{"create", "c1", "app"}, nil, "a password is required", nil},
		{"update с пустым паролем", []string{"update", "c1", "app", "--password-env", "EMPTY"}, nil, "a password is required", nil},
		{"create ошибка POST", []string{"create", "c1", "app", "--password-stdin"}, nil, "API 404", []string{"POST /v1/clusters/c1/users"}},
		{"update ошибка GET", []string{"update", "c1", "app", "--password-stdin"}, nil, "API 404", []string{"GET /v1/clusters/c1/users/app"}},
		{"update неверные роли", []string{"update", "c1", "app", "--password-stdin"}, map[string]string{"GET /v1/clusters/c1/users/app": `{"roles":42}`}, "parse response", []string{"GET /v1/clusters/c1/users/app"}},
		{"update ошибка PUT", []string{"update", "c1", "app", "--role", "reader"}, nil, "API 404", []string{"PUT /v1/clusters/c1/users/app"}},
		{"delete ошибка DELETE", []string{"delete", "c1", "app", "--yes"}, nil, "API 404", []string{"DELETE /v1/clusters/c1/users/app"}},
		{"get ошибка GET", []string{"get", "c1", "app"}, nil, "API 404", []string{"GET /v1/clusters/c1/users/app"}},
		{"list ошибка GET", []string{"list", "c1"}, nil, "API 404", []string{"GET /v1/clusters/c1/users"}},
		{"get неверные роли", []string{"get", "c1", "app"}, map[string]string{"GET /v1/clusters/c1/users/app": `{"roles":42}`}, "parse response", []string{"GET /v1/clusters/c1/users/app"}},
		{"list неверные роли", []string{"list", "c1"}, map[string]string{"GET /v1/clusters/c1/users": `{"users":[{"roles":42}]}`}, "parse response", []string{"GET /v1/clusters/c1/users"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(tc.routes)
			out, _, err := runWith(t, f, false, func(_ *state.State, cmd *cobra.Command) {
				cmd.SetIn(strings.NewReader("secret\n"))
			}, tc.args...)
			require.ErrorContains(t, err, tc.want)
			assert.Empty(t, out)
			assert.Equal(t, tc.requests, f.requests())
		})
	}
}
