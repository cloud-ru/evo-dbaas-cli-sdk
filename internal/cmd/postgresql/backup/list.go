package backup

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func listCmd(s *state.State) *cobra.Command {
	var cluster string

	cmd := base.ListCmd{
		Use: "list", Short: i18n.T("postgresql.backup.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.Backups(ctx, cluster) })
		},
	}.Cmd(s)
	cmd.Flags().StringVar(&cluster, "cluster-id", "", i18n.T("postgresql.backup.flag.cluster"))
	_ = cmd.MarkFlagRequired("cluster-id")

	return cmd
}
