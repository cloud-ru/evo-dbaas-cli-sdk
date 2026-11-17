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

func getCmd(s *state.State) *cobra.Command {
	var cluster string

	cmd := base.ListCmd{
		Use: "get NAME", Short: i18n.T("opensearch.snapshot.get.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.OpenSearch) ([]byte, error) { return h.Snapshot(ctx, cluster, args[0]) })
		},
		Table: printSnapshot,
	}.Cmd(s)
	cmd.Flags().StringVar(&cluster, "cluster-id", "", i18n.T("opensearch.snapshot.flag.cluster"))
	_ = cmd.MarkFlagRequired("cluster-id")

	return cmd
}
