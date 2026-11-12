package database

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(t.Context())

	return out.String(), stderr.String(), err
}

// uint32 size_mb и локали должны переживать human-декодирование;
// при --json неизвестное поле и большое число остаются как в API.
func TestDatabaseResponseFields(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"list", "get"} {
		for _, jsonOut := range []bool{false, true} {
			raw := `{"name":"db","owner":"owner","size_mb":4294967295,"lc_collate":"C","lc_ctype":"POSIX","future":12345678901234567890}`

			args := []string{verb, "c"}
			if verb == "list" {
				raw = `{"databases":[` + raw + `]}`
			} else {
				args = append(args, "db")
			}

			out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, raw) }, func(s *state.State) { s.Printer.JSON = jsonOut }, args...)
			require.NoError(t, err)

			if jsonOut {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Contains(t, out, "owner")

				if verb == "list" {
					assert.Contains(t, out, "4294967295")
					assert.Contains(t, out, "POSIX")
				}
			}
		}
	}
}

func TestDatabaseBoundaryFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args        []string
		body, want  string
		clientError bool
	}{
		{[]string{"list", "c"}, `{"databases":false}`, "parse response", false},
		{[]string{"get", "c", "db"}, `{"owner":[]}`, "parse response", false},
		{[]string{"create", "c", "db"}, `{}`, "owner", false},
		{[]string{"create", "c", "db", "--owner", "u", "--body", "@/missing-pg-body"}, `{}`, "--body", false},
		{[]string{"delete", "c", "db"}, `{}`, "--yes", false},
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

	for _, args := range [][]string{{"create", "c", "db", "--owner", "u"}, {"delete", "c", "db", "--yes"}} {
		out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "busy", http.StatusConflict) }, nil, args...)
		require.ErrorContains(t, err, "busy")
		assert.Empty(t, out)
	}

	for _, args := range [][]string{{"create", "c", "db", "--owner", "u"}, {"delete", "c", "db", "--yes"}} {
		out, stderr, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"future":12345678901234567890}`)
		}, func(s *state.State) { s.Printer.JSON = true }, args...)
		require.NoError(t, err)
		assert.Contains(t, out, "12345678901234567890")
		assert.Empty(t, stderr)
	}
}
