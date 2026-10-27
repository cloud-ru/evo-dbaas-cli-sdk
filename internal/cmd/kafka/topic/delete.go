package topic

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func deleteCmd(s *state.State) *cobra.Command {
	return base.DeleteCmd{
		Use: "delete CLUSTER_ID TOPIC", Short: i18n.T("kafka.topic.delete.short"), Args: cobra.ExactArgs(2),
		Delete: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.DeleteTopic(ctx, args[0], args[1]) })
		},
		Ops: operation.Ops, OpsCmd: operation.GetCmd,
	}.Cmd(s)
}
