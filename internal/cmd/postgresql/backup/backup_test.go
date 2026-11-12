package backup

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// Сетка создаёт копию без ожидания. Здесь — обязательный cluster-id до сети,
// ошибка API и --wait, который должен опрашивать именно операции PostgreSQL.
func TestBackupCommands(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		args     []string
		want     string
		status   int
		requests []string
	}{
		{name: "list requires cluster", args: []string{"list"}, want: "cluster-id"},
		{name: "get requires cluster", args: []string{"get", "daily"}, want: "cluster-id"},
		{name: "create requires cluster", args: []string{"create", "daily"}, want: "cluster-id"},
		{name: "get requires name", args: []string{"get", "--cluster-id", "c1"}, want: "accepts 1 arg"},
		{name: "create requires name", args: []string{"create", "--cluster-id", "c1"}, want: "accepts 1 arg"},
		{name: "get rejects extra argument", args: []string{"get", "daily", "extra", "--cluster-id", "c1"}, want: "accepts 1 arg(s), received 2"},
		{name: "create rejects extra argument", args: []string{"create", "daily", "extra", "--cluster-id", "c1"}, want: "accepts 1 arg(s), received 2"},
		{name: "list API error", args: []string{"list", "--cluster-id", "c1"}, want: "busy", status: http.StatusConflict, requests: []string{"GET /v1/clusters/c1/backups"}},
		{name: "get API error", args: []string{"get", "daily", "--cluster-id", "c1"}, want: "busy", status: http.StatusConflict, requests: []string{"GET /v1/clusters/c1/backups/daily"}},
		{name: "create API error", args: []string{"create", "daily", "--cluster-id", "c1"}, want: "busy", status: http.StatusConflict, requests: []string{"POST /v1/clusters/c1/backups"}},
		{name: "create wait", args: []string{"create", "daily", "--cluster-id", "c1", "--wait"}, requests: []string{"POST /v1/clusters/c1/backups", "GET /v1/operations/op1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				requests []string
				mu       sync.Mutex
			)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()

				requests = append(requests, r.Method+" "+r.URL.Path)
				mu.Unlock()

				if tc.status != 0 {
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, `{"message":"busy"}`)

					return
				}

				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{"id":"op1","done":true}`)
					return
				}

				_, _ = io.WriteString(w, `{"id":"op1","done":false}`)
			}))
			t.Cleanup(srv.Close)
			client, err := cloud.NewPostgreSQL(srv.URL, cloud.Options{HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil }})
			require.NoError(t, err)

			var out bytes.Buffer

			s := &state.State{PostgreSQL: func() (*cloud.PostgreSQL, error) { return client, nil }, Printer: &output.Printer{Out: &out, Err: io.Discard, JSON: true}, Timeout: time.Second, PollDelay: time.Millisecond}
			cmd := NewCmd(s)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)

			err = cmd.ExecuteContext(t.Context())
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.Empty(t, out.String())
			} else {
				require.NoError(t, err)
				assert.JSONEq(t, `{"id":"op1","done":true}`, out.String())
			}

			mu.Lock()
			assert.Equal(t, tc.requests, requests)
			mu.Unlock()
		})
	}
}
