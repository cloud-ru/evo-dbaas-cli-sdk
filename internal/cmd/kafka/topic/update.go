package topic

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func updateCmd(s *state.State) *cobra.Command {
	var (
		u      cloud.KafkaTopicUpdate
		params []string
		wait   bool
	)

	cmd := &cobra.Command{
		Use: "update CLUSTER_ID TOPIC", Short: i18n.T("kafka.topic.update.short"), Args: cobra.ExactArgs(2),
		Long: i18n.T("kafka.topic.update.long"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if u.Partitions == 0 && u.Replication == 0 && len(params) == 0 {
				return &base.ExitError{Code: 1, Msg: i18n.T("kafka.topic.update.err.no_changes")}
			}

			var err error
			if u.Params, err = base.ParseKV("param", params); err != nil {
				return err
			}

			raw, err := operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.UpdateTopic(cmd.Context(), args[0], args[1], u) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	cmd.Flags().Int32Var(&u.Partitions, "partitions", 0, i18n.T("kafka.topic.update.flag.partitions"))
	cmd.Flags().Int32Var(&u.Replication, "replication", 0, i18n.T("kafka.topic.update.flag.replication"))
	cmd.Flags().StringArrayVar(&params, "param", nil, i18n.T("kafka.topic.flag.param"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("kafka.cluster.flag.wait"))

	return cmd
}
