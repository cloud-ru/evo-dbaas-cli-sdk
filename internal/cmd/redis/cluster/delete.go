package cluster

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func deleteCmd(s *state.State) *cobra.Command {
	return base.DeleteCmd{
		Use: "delete CLUSTER_ID", Short: i18n.T("redis.cluster.delete.short"), Args: cobra.ExactArgs(1),
		Delete: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(r *cloud.Redis) ([]byte, error) { return r.DeleteCluster(ctx, args[0]) })
		},
		Ops: operation.Ops, OpsCmd: operation.GetCmd,
	}.Cmd(s)
}
