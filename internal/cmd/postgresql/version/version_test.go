package version

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

func TestVersionObjects(t *testing.T) {
	t.Parallel()

	raw := `{"versions":[{"name":"17","future":12345678901234567890}]}`
	for _, jsonOut := range []bool{false, true} {
		out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, raw) }, func(s *state.State) { s.Printer.JSON = jsonOut }, "list")
		require.NoError(t, err)

		if jsonOut {
			assert.JSONEq(t, raw, out)
		} else {
			assert.Equal(t, "17\n", out)
		}
	}

	out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"versions":true}`) }, nil, "list")
	require.ErrorContains(t, err, "parse response")
	assert.Empty(t, out)
}

// Сетка вызывает locales с одной версией; лишний аргумент должен отвергаться
// до создания клиента, а не молча игнорироваться.
func TestVersionLocalesRejectsExtraArgument(t *testing.T) {
	t.Parallel()

	called := false
	cmd := NewCmd(&state.State{PostgreSQL: func() (*cloud.PostgreSQL, error) {
		called = true
		return nil, errors.New("unexpected client initialization")
	}})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"locales", "17", "extra"})
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "accepts 1 arg(s), received 2")
	assert.Equal(t, 1, output.ExitCode(err))
	assert.False(t, called)
}
