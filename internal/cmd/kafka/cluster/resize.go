package cluster

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func resizeCmd(s *state.State) *cobra.Command {
	var (
		u      cloud.KafkaClusterUpdate
		params []string
		body   string
		wait   bool
	)

	cmd := &cobra.Command{
		Use: "resize CLUSTER_ID", Short: i18n.T("kafka.cluster.resize.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkController(cmd, u.Controller); err != nil {
				return err
			}

			if u.Controller.Spec == "" && u.Spec == "" && u.StorageGiB == 0 && len(params) == 0 && body == "" {
				return &base.ExitError{Code: 1, Msg: i18n.T("kafka.cluster.resize.err.no_changes")}
			}

			var err error
			if u.Params, err = base.ParseKV("param", params); err != nil {
				return err
			}

			if u.Body, err = base.Body(body); err != nil {
				return err
			}

			raw, err := operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.UpdateCluster(cmd.Context(), args[0], u) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	cmd.Flags().StringVar(&u.Controller.Spec, "controller-spec", "", i18n.T("kafka.cluster.resize.flag.controller_spec"))
	cmd.Flags().Int32Var(&u.Controller.StorageGiB, "controller-storage-gib", 0, i18n.T("kafka.cluster.resize.flag.controller_storage_gib"))
	cmd.Flags().StringVar(&u.Spec, "spec", "", i18n.T("kafka.cluster.resize.flag.spec"))
	cmd.Flags().Int32Var(&u.StorageGiB, "storage-gib", 0, i18n.T("kafka.cluster.resize.flag.storage_gib"))
	cmd.Flags().StringArrayVar(&params, "param", nil, i18n.T("kafka.cluster.resize.flag.param"))
	cmd.Flags().StringVar(&body, "body", "", base.BodyHelpText())
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("kafka.cluster.flag.wait"))

	return cmd
}
