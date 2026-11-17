package snapshot

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func listCmd(s *state.State) *cobra.Command {
	var cluster string

	cmd := base.ListCmd{
		Use: "list", Short: i18n.T("opensearch.snapshot.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.OpenSearch) ([]byte, error) { return h.Snapshots(ctx, cluster) })
		},
		Table: printSnapshots,
	}.Cmd(s)
	cmd.Flags().StringVar(&cluster, "cluster-id", "", i18n.T("opensearch.snapshot.flag.cluster"))
	_ = cmd.MarkFlagRequired("cluster-id")

	return cmd
}
