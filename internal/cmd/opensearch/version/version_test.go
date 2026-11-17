package version

import (
	"bytes"
	"context"
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

func runOS(t *testing.T, handler http.HandlerFunc, setup func(*state.State), args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := cloud.NewOpenSearch(srv.URL, cloud.Options{HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil }})
	require.NoError(t, err)

	var out, stderr bytes.Buffer

	s := &state.State{OpenSearch: func() (*cloud.OpenSearch, error) { return client, nil }, Printer: &output.Printer{Out: &out, Err: &stderr}, Timeout: time.Second, PollDelay: time.Millisecond, Env: func(string) string { return "secret" }}
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

// Непустой human-список и битая схема: golden получает пустой список, cloud не разбирает версии.
func TestVersionObjects(t *testing.T) {
	t.Parallel()

	raw := `{"versions":[{"name":"2.18.0","future":12345678901234567890}]}`
	out, _, err := runOS(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, raw) }, nil, "list")
	require.NoError(t, err)
	assert.Equal(t, "2.18.0\n", out)

	out, _, err = runOS(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"versions":true}`) }, nil, "list")
	require.ErrorContains(t, err, "parse response")
	assert.Empty(t, out)
}
