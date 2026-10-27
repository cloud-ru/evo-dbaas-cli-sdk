package base

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func TestListWithoutTablePrintsJSONForHumans(t *testing.T) {
	t.Parallel()

	const raw = `{"versions":[{"id":"v7","future":17}]}`

	f := newFake(map[string]string{"GET /v1/versions": raw})
	out, stderr, err := run(t, f, false, func(s *state.State) *cobra.Command {
		return (ListCmd{Use: "list", Args: cobra.NoArgs, List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			r, err := s.Redis()
			if err != nil {
				return nil, err
			}

			return r.Versions(ctx)
		}}).Cmd(s)
	})
	require.NoError(t, err)
	assert.JSONEq(t, raw, out)
	assert.Empty(t, stderr)
	assert.Equal(t, []string{"GET /v1/versions"}, f.requests())
}

func TestFinishOpWithoutHintReportsPendingAndKeepsOperation(t *testing.T) {
	t.Parallel()

	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "human", true: "json"}[json], func(t *testing.T) {
			t.Parallel()

			const raw = `{"id":"op1"}`

			f := newFake(nil)
			out, stderr, err := run(t, f, json, func(s *state.State) *cobra.Command {
				return &cobra.Command{Use: "wait", RunE: func(cmd *cobra.Command, _ []string) error {
					op, ok := cloud.ParseOperation([]byte(raw))
					return FinishOp(cmd, s, op, ok, true, redisOps, "")
				}}
			})
			require.Error(t, err)
			assert.Equal(t, 8, output.ExitCode(err))
			require.ErrorContains(t, err, "op1")
			require.ErrorContains(t, err, "no route")
			assert.NotContains(t, err.Error(), "operations get")
			assert.Empty(t, stderr)

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Empty(t, out)
			}

			assert.Equal(t, []string{"GET /v1/operations/op1"}, f.requests())
		})
	}
}
