package user

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func runPG(t *testing.T, handler http.HandlerFunc, setup func(*state.State), args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := cloud.NewPostgreSQL(srv.URL, cloud.Options{HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil }})
	require.NoError(t, err)

	var out, stderr bytes.Buffer

	s := &state.State{PostgreSQL: func() (*cloud.PostgreSQL, error) { return client, nil }, Printer: &output.Printer{Out: &out, Err: &stderr}, Timeout: time.Second, PollDelay: time.Millisecond, Env: func(string) string { return "secret" }}
	if setup != nil {
		setup(s)
	}

	cmd := NewCmd(s)
	cmd.SetIn(strings.NewReader("secret\n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(t.Context())

	return out.String(), stderr.String(), err
}

// Роли PostgreSQL — массив объектов, а не ClickHouse RepeatedString;
// здесь две роли, supported_roles в list и большое неизвестное число в JSON;
// типовые golden этих краёв не содержат.
func TestUserRoleObjects(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"list", "get"} {
		for _, jsonOut := range []bool{false, true} {
			raw := `{"name":"u","granted_roles":[{"name":"reader"},{"name":"writer"}],"supported_roles":[{"name":"owner"}],"future":12345678901234567890}`

			args := []string{verb, "c1"}
			if verb == "list" {
				raw = `{"users":[` + raw + `]}`
			} else {
				args = append(args, "u")
			}

			out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, raw) }, func(s *state.State) { s.Printer.JSON = jsonOut }, args...)
			require.NoError(t, err)

			if jsonOut {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Contains(t, out, "reader,writer")

				if verb == "list" {
					assert.Contains(t, out, "owner")
				}
			}
		}
	}
}

// Пустая роль должна очищать granted_roles, а смена пароля не должна стирать роли.
func TestUserUpdateClearsOrKeepsRoles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"update", "c1", "u", "--role="}, `{"cluster_id":"c1","name":"u","granted_roles":[]}`},
		{[]string{"update", "c1", "u", "--password-env", "PW"}, `{"cluster_id":"c1","name":"u","password":"secret"}`},
		{[]string{"create", "c1", "u", "--password-stdin"}, `{"cluster_id":"c1","name":"u","password":"secret"}`},
	} {
		args := tc.args

		raw := `{"future":12345678901234567890}`
		out, stderr, err := runPG(t, func(w http.ResponseWriter, r *http.Request) {
			b, e := io.ReadAll(r.Body)
			assert.NoError(t, e)
			assert.JSONEq(t, tc.want, string(b))

			_, _ = io.WriteString(w, raw)
		}, func(s *state.State) { s.Printer.JSON = true }, args...)
		require.NoError(t, err)
		assert.JSONEq(t, raw, out)
		assert.Empty(t, stderr)
	}
}

func TestUserBoundaryFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args        []string
		body, want  string
		clientError bool
	}{
		{[]string{"list", "c"}, `{"users":false}`, "parse response", false},
		{[]string{"get", "c", "u"}, `{"name":[]}`, "parse response", false},
		{[]string{"create", "c", "u"}, `{}`, "password is required", false},
		{[]string{"create", "c", "u", "--password-env", "PW", "--body", "@/missing-pg-body"}, `{}`, "--body", false},
		{[]string{"update", "c", "u"}, `{}`, "specify", false},
		{[]string{"update", "c", "u", "--password-env", "PW", "--body", "@/missing-pg-body"}, `{}`, "--body", false},
		{[]string{"delete", "c", "u"}, `{}`, "--yes", false},
		{[]string{"list", "c"}, `{}`, "client unavailable", true},
	} {
		out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) }, func(s *state.State) {
			if tc.clientError {
				s.PostgreSQL = func() (*cloud.PostgreSQL, error) { return nil, errors.New("client unavailable") }
			}
		}, tc.args...)
		require.ErrorContains(t, err, tc.want)
		assert.Empty(t, out)
	}

	for _, args := range [][]string{{"create", "c", "u", "--password-env", "PW"}, {"update", "c", "u", "--role", "r"}, {"delete", "c", "u", "--yes"}} {
		out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "busy", http.StatusConflict) }, nil, args...)
		require.ErrorContains(t, err, "busy")
		assert.Empty(t, out)
	}
}
