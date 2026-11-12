package cluster

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// Каждый глагол обязан подключать --yes и --wait; общий base-тест
// не поймает забытый DeleteCmd или неправильный Ops у нового глагола.
func TestClusterLifecycleConfirmation(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"suspend", "resume", "switchover"} {
		for _, tc := range []struct {
			name          string
			flags         []string
			refused, poll bool
		}{
			{name: "without yes", refused: true},
			{name: "explicit false", flags: []string{"--yes=false"}, refused: true},
			{name: "yes", flags: []string{"--yes"}},
			{name: "wait needs yes", flags: []string{"--wait"}, refused: true},
			{name: "yes and wait", flags: []string{"--yes", "--wait"}, poll: true},
		} {
			t.Run(verb+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				route := "POST /v1/clusters/c1:" + verb
				f := newFake(map[string]string{route: `{"id":"op1","done":false}`, "GET /v1/operations/op1": `{"id":"op1","done":true}`})

				out, _, err := runWith(t, f, true, func(_ *state.State, cmd *cobra.Command) {
					action, _, err := cmd.Find([]string{verb})
					require.NoError(t, err)

					flag := action.Flags().Lookup("yes")
					require.NotNil(t, flag)
					assert.Equal(t, i18n.T("postgresql.cluster."+verb+".flag.yes"), flag.Usage)
				}, append([]string{verb, "c1"}, tc.flags...)...)
				if tc.refused {
					require.ErrorContains(t, err, "--yes")
					require.EqualError(t, err, i18n.T("postgresql.cluster."+verb+".err.need_yes"))
					assert.Equal(t, 1, output.ExitCode(err))
					assert.Empty(t, f.requests())
					assert.Empty(t, out)

					return
				}

				require.NoError(t, err)
				assert.Contains(t, out, "op1")

				want := []string{route}
				if tc.poll {
					want = append(want, "GET /v1/operations/op1")
				}

				assert.Equal(t, want, f.requests())
			})
		}
	}
}
