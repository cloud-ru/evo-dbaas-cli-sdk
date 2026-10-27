package topic

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		partitions, replication int32
		params                  []string
		wait                    bool
	)

	cmd := &cobra.Command{
		Use: "create CLUSTER_ID TOPIC", Short: i18n.T("kafka.topic.create.short"), Args: cobra.ExactArgs(2),
		Long: i18n.T("kafka.topic.create.long"),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := base.ParseKV("param", params)
			if err != nil {
				return err
			}

			raw, err := operation.With(s, func(k *cloud.Kafka) ([]byte, error) {
				return k.CreateTopic(cmd.Context(), args[0], args[1], partitions, replication, p)
			})
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	cmd.Flags().Int32Var(&partitions, "partitions", 1, i18n.T("kafka.topic.create.flag.partitions"))
	cmd.Flags().Int32Var(&replication, "replication", 1, i18n.T("kafka.topic.create.flag.replication"))
	cmd.Flags().StringArrayVar(&params, "param", nil, i18n.T("kafka.topic.flag.param"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("kafka.cluster.flag.wait"))

	return cmd
}
