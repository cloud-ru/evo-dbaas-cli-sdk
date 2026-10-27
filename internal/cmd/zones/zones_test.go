package zones

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func TestList(t *testing.T) {
	t.Parallel()

	const raw = `[{"id":"z1","name":"ru.AZ-1","default":true,"future":17}]`

	clientErr := errors.New("compute unavailable")
	for _, tc := range []struct {
		name, body, wantErr string
		status              int
		json, clientFailure bool
	}{
		{name: "table", body: raw, status: http.StatusOK},
		{name: "json", body: raw, status: http.StatusOK, json: true},
		{name: "null table", body: `null`, status: http.StatusOK},
		{name: "null json", body: `null`, status: http.StatusOK, json: true},
		{name: "not array", body: `{"items":[]}`, status: http.StatusOK, wantErr: "cannot parse the zone list"},
		{name: "api", body: `{"message":"denied"}`, status: http.StatusForbidden, wantErr: "zones: API 403: denied"},
		{name: "client", clientFailure: true, wantErr: "compute unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, "GET /api/v1/availability-zones", r.Method+" "+r.URL.Path)
				assert.Empty(t, r.URL.RawQuery)
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Empty(t, body)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			c, err := cloud.NewCompute(srv.URL, cloud.Options{HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil }})
			require.NoError(t, err)

			var out, stderr bytes.Buffer

			s := &state.State{Printer: &output.Printer{Out: &out, Err: &stderr, JSON: tc.json}, Compute: func() (*cloud.Compute, error) { return c, nil }}
			if tc.clientFailure {
				s.Compute = func() (*cloud.Compute, error) { return nil, clientErr }
			}

			cmd := NewCmd(s)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"list"})

			err = cmd.ExecuteContext(t.Context())
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)

				code := 1
				if tc.status == http.StatusForbidden {
					code = 4
				}

				assert.Equal(t, code, output.ExitCode(err))
				assert.Empty(t, out.String())

				if tc.name == "not array" {
					var ee *base.ExitError
					require.ErrorAs(t, err, &ee)
					assert.Equal(t, 1, ee.Code)
				}

				if tc.clientFailure {
					require.ErrorIs(t, err, clientErr)
				}
			} else {
				require.NoError(t, err)

				if tc.json {
					assert.JSONEq(t, tc.body, out.String())
				} else if tc.body == "null" {
					// Текущее поведение: null принимается как пустой список зон.
					assert.Equal(t, []string{"ID", "NAME", "DEFAULT"}, strings.Fields(out.String()))
				} else {
					assert.Equal(t, []string{"ID", "NAME", "DEFAULT", "z1", "ru.AZ-1", "true"}, strings.Fields(out.String()))
				}
			}

			if tc.clientFailure {
				assert.Zero(t, calls.Load())
			} else {
				assert.Equal(t, int32(1), calls.Load())
			}

			assert.Empty(t, stderr.String())
		})
	}
}
