package cluster

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func resizeCmd(s *state.State) *cobra.Command {
	var (
		u      cloud.ClickHouseClusterUpdate
		config []string
		body   string
		wait   bool
	)

	cmd := &cobra.Command{
		Use:   "resize CLUSTER_ID",
		Short: i18n.T("clickhouse.cluster.resize.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if u.Spec == "" && u.StorageGiB == 0 && u.Keeper.Spec == "" && u.Keeper.StorageGiB == 0 && len(config) == 0 && body == "" {
				return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.cluster.resize.err.no_changes")}
			}

			kv, err := base.ParseKV("config", config)
			if err != nil {
				return err
			}

			if len(kv) > 0 {
				if u.Config, err = cloud.ParseClickHouseConfig("config", kv); err != nil {
					return &base.ExitError{Code: 1, Msg: err.Error()}
				}
			}

			if u.Body, err = base.Body(body); err != nil {
				return err
			}

			raw, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.UpdateCluster(cmd.Context(), args[0], u) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	cmd.Flags().StringVar(&u.Spec, "spec", "", i18n.T("clickhouse.cluster.resize.flag.spec"))
	cmd.Flags().Int32Var(&u.StorageGiB, "storage-gib", 0, i18n.T("clickhouse.cluster.resize.flag.storage_gib"))
	keeperFlags(cmd, &u.Keeper)
	cmd.Flags().StringArrayVar(&config, "config", nil, i18n.T("clickhouse.cluster.resize.flag.config"))
	cmd.Flags().StringVar(&body, "body", "", base.BodyHelpText())
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("clickhouse.cluster.flag.wait"))

	return cmd
}
