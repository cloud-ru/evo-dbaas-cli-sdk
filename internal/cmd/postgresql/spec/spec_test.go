package spec

import (
	"bytes"
	"context"
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
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(t.Context())

	return out.String(), stderr.String(), err
}

func TestSpecificationDeploymentFields(t *testing.T) {
	t.Parallel()

	raw := `{"specifications":[{"id":"s","display_name":"pg","cpu":1.5,"memory":4,"deployment_mode":"STANDARD","max_hosts":2,"min_storage_gb":50,"future":12345678901234567890}]}`
	for _, jsonOut := range []bool{false, true} {
		out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, raw) }, func(s *state.State) { s.Printer.JSON = jsonOut }, "list", "--version", "17")
		require.NoError(t, err)

		if jsonOut {
			assert.JSONEq(t, raw, out)
		} else {
			assert.Equal(t, []string{"ID", "NAME", "CPU", "RAM_GB", "MODE", "MAX_HOSTS", "MIN_STORAGE", "s", "pg", "1.5", "4", "STANDARD", "2", "50"}, strings.Fields(out))
		}
	}

	out, _, err := runPG(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"specifications":true}`) }, nil, "list", "--version", "17")
	require.ErrorContains(t, err, "parse response")
	assert.Empty(t, out)
}

func TestSpecListNeedsVersionOrCluster(t *testing.T) {
	t.Parallel()

	called := false
	_, _, err := runPG(t, func(http.ResponseWriter, *http.Request) { called = true }, nil, "list")
	require.Error(t, err)
	assert.Equal(t, 1, output.ExitCode(err))
	assert.Contains(t, err.Error(), "--version")
	assert.False(t, called, "без --version и --cluster-id запрос не уходит")
}
