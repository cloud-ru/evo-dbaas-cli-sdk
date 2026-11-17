package cluster

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func deleteCmd(s *state.State) *cobra.Command {
	return base.DeleteCmd{
		Use: "delete CLUSTER_ID", Short: i18n.T("opensearch.cluster.delete.short"), Args: cobra.ExactArgs(1),
		Delete: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.OpenSearch) ([]byte, error) { return h.DeleteCluster(ctx, args[0]) })
		},
		Ops: operation.Ops, OpsCmd: operation.GetCmd, ClusterCmd: "opensearch clusters get",
	}.Cmd(s)
}
