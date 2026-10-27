package cluster

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func describeCmd(s *state.State) *cobra.Command {
	return base.DescribeCmd{
		Use: "get CLUSTER_ID", Short: i18n.T("clickhouse.cluster.get.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Cluster(ctx, args[0]) })
		},
		Table: printCluster,
	}.Cmd(s)
}
