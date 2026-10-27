package clickhouse

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func TestNewCmdRegistersEightResources(t *testing.T) {
	t.Parallel()

	cmd := NewCmd(&state.State{}, "https://clickhouse.example")

	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}

	assert.ElementsMatch(t, []string{"clusters", "operations", "roles", "specs", "auth", "databases", "users", "versions"}, names)
	assert.Len(t, cmd.Commands(), 8)
}

func TestAPIURLUsesConfiguredDefaultAndWritesFlagToState(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		defaultURL string
		args       []string
		want       string
	}{
		{"адрес из аргумента", "https://configured.example", nil, "https://configured.example"},
		{"флаг заменяет адрес", "https://configured.example", []string{"--api-url", "https://override.example"}, "https://override.example"},
		{"пустой адрес по умолчанию", "", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := &state.State{ClickHouseURL: "старое значение"}
			cmd := NewCmd(s, tc.defaultURL)
			assert.Equal(t, tc.defaultURL, s.ClickHouseURL)

			flag := cmd.PersistentFlags().Lookup("api-url")
			require.NotNil(t, flag)
			assert.Equal(t, tc.defaultURL, flag.DefValue)
			// Справка проверяет флаг без создания клиента API.
			cmd.SetArgs(append(append([]string(nil), tc.args...), "versions", "list", "--help"))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			assert.Equal(t, tc.want, s.ClickHouseURL)
		})
	}
}

func TestRootRoutesAliasAndPersistentFlagsToAPI(t *testing.T) {
	t.Parallel()

	for _, json := range []bool{false, true} {
		var calls atomic.Int32

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)

			if !assert.Equal(t, "GET /v1/versions", r.Method+" "+r.URL.Path) {
				http.Error(w, "no route", http.StatusNotFound)
				return
			}

			assert.Empty(t, r.URL.RawQuery)
			b, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Empty(t, b)

			_, _ = io.WriteString(w, `{"versions":[{"name":"25.8","future":17}]}`)
		}))
		t.Cleanup(srv.Close)

		var out, stderr bytes.Buffer

		s := &state.State{Printer: &output.Printer{Out: &out, Err: &stderr}}
		s.ClickHouse = func() (*cloud.ClickHouse, error) {
			return cloud.NewClickHouse(s.ClickHouseURL, cloud.Options{HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil }})
		}
		root := &cobra.Command{Use: "cloudru-dbaas"}
		root.PersistentFlags().BoolVar(&s.Printer.JSON, "json", false, "JSON")
		root.AddCommand(NewCmd(s, "https://unused.example"))
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)

		args := []string{"ch", "--api-url", srv.URL, "versions", "list"}
		if json {
			args = append(args, "--json")
		}

		root.SetArgs(args)
		require.NoError(t, root.ExecuteContext(t.Context()))

		if json {
			assert.JSONEq(t, `{"versions":[{"name":"25.8","future":17}]}`, out.String())
		} else {
			assert.Equal(t, "25.8\n", out.String())
		}

		assert.Empty(t, stderr.String())
		assert.Equal(t, srv.URL, s.ClickHouseURL)
		assert.Equal(t, int32(1), calls.Load())
	}
}
