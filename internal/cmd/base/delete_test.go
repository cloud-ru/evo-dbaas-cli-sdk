package base

import (
	"context"
	"errors"
	"fmt"
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

// Находка 12.11: delete во время создания возвращает API 400/500 с отказом
// планировщика. Подсказка добавляется только к API-ошибке удаления кластера,
// исходная ошибка остаётся второй строкой, её код выхода сохраняется.
func TestDeleteClusterCreatingError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, message, clusterCmd string
		status                    int
		hint, local, wrapped      bool
	}{
		{"inventory 400", "create inventory task 123: FailedPrecondition", "redis clusters get", 400, true, false, false},
		{"tasks 500", "cannot receive any new tasks", "redis clusters get", 500, true, false, false},
		{"код доступа", "cannot receive any new tasks", "redis clusters get", 403, true, false, false},
		{"обёрнутая API-ошибка", "cannot receive any new tasks", "redis clusters get", 500, true, false, true},
		{"другая причина API", "cluster not found", "redis clusters get", 404, false, false, false},
		{"другой ресурс", "cannot receive any new tasks", "", 500, false, false, false},
		{"не API-ошибка", "cannot receive any new tasks", "redis clusters get", 500, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "DELETE", r.Method)
				assert.Equal(t, "/v1/clusters/c1", r.URL.Path)
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"message":%q}`, tc.message)
			}))
			t.Cleanup(srv.Close)

			r, err := cloud.NewRedis(srv.URL, cloud.Options{
				HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil },
			})
			require.NoError(t, err)

			var original error

			cmd := (DeleteCmd{
				Use: "delete ID", Args: cobra.ExactArgs(1), ClusterCmd: tc.clusterCmd,
				Delete: func(ctx context.Context, _ *state.State, args []string) ([]byte, error) {
					if tc.local {
						original = errors.New(tc.message)
						return nil, original
					}

					raw, err := r.DeleteCluster(ctx, args[0])
					if tc.wrapped {
						err = fmt.Errorf("delete: %w", err)
					}

					original = err

					return raw, err
				},
			}).Cmd(&state.State{Printer: &output.Printer{Out: io.Discard, Err: io.Discard}})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"c1", "--yes"})
			err = cmd.ExecuteContext(t.Context())
			require.Error(t, err)
			require.Error(t, original)
			assert.Equal(t, output.ExitCode(original), output.ExitCode(err))

			if tc.local {
				assert.Zero(t, calls.Load())
			} else {
				assert.Equal(t, int32(1), calls.Load())
			}

			if tc.hint {
				var exit *ExitError
				require.ErrorAs(t, err, &exit)
				assert.Equal(t, "The cluster is still being created; deletion is available after creation completes; check its state: `redis clusters get c1`\n"+original.Error(), exit.Msg)
			} else {
				assert.Same(t, original, err)
			}
		})
	}
}
