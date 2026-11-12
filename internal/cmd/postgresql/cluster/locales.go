package cluster

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func localesCmd(s *state.State) *cobra.Command {
	return base.ListCmd{
		Use: "locales CLUSTER_ID", Short: i18n.T("postgresql.cluster.locales.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.ClusterLocales(ctx, args[0]) })
		},
	}.Cmd(s)
}
