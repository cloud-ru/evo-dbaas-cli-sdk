package operation

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

func TestOperationDatesAndWait(t *testing.T) {
	t.Parallel()

	raw := `{"operations":[{"id":"op1","done":true,"created_at":"2026-11-07T10:00:00Z","description":"done"},{"id":"op2","done":false}]}`
	for _, jsonOut := range []bool{false, true} {
		out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, raw) }, func(s *state.State) { s.Printer.JSON = jsonOut }, "list", "c1")
		require.NoError(t, err)

		if jsonOut {
			assert.JSONEq(t, raw, out)
		} else {
			assert.Contains(t, out, "2026-11-07T10:00:00Z")
			assert.Contains(t, out, "op2")
		}
	}

	out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"operations":[{"created_at":"bad"}]}`)
	}, nil, "list", "c1")
	require.ErrorContains(t, err, "parse response")
	assert.Empty(t, out)
	out, _, err = runPG(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "missing", http.StatusNotFound) }, nil, "get", "missing", "--wait")
	require.ErrorContains(t, err, "missing")
	assert.Equal(t, 1, output.ExitCode(err))
	assert.Empty(t, out)
	out, _, err = runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"id":"op1","done":true}`) }, nil, "get", "op1", "--wait")
	require.NoError(t, err)
	assert.Contains(t, out, "op1")

	s := &state.State{PostgreSQL: func() (*cloud.PostgreSQL, error) { return nil, errors.New("client unavailable") }}
	getter, err := Ops(s)
	require.ErrorContains(t, err, "client unavailable")
	assert.Nil(t, getter)
	out, _, err = runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{}`) }, func(s *state.State) {
		s.PostgreSQL = func() (*cloud.PostgreSQL, error) { return nil, errors.New("client unavailable") }
	}, "get", "op1")
	require.ErrorContains(t, err, "client unavailable")
	assert.Empty(t, out)
}
