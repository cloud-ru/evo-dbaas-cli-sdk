package cluster

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func resizeCmd(s *state.State) *cobra.Command {
	var (
		u      cloud.RedisClusterUpdate
		params []string
		body   string
		wait   bool
	)

	cmd := &cobra.Command{
		Use: "resize CLUSTER_ID", Short: i18n.T("redis.cluster.resize.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if u.Spec == "" && u.StorageGiB == 0 && len(params) == 0 && body == "" {
				return &base.ExitError{Code: 1, Msg: i18n.T("redis.cluster.resize.err.no_changes")}
			}
			// API: 400 «conflicting update fields: storage_gib, specification_id» (стенд 14.10).
			if u.Spec != "" && u.StorageGiB > 0 {
				return &base.ExitError{Code: 1, Msg: i18n.T("redis.cluster.resize.err.conflicting_fields")}
			}

			var err error
			if u.Params, err = base.ParseKV("param", params); err != nil {
				return err
			}

			if u.Body, err = base.Body(body); err != nil {
				return err
			}

			raw, err := operation.With(s, func(r *cloud.Redis) ([]byte, error) { return r.UpdateCluster(cmd.Context(), args[0], u) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	cmd.Flags().StringVar(&u.Spec, "spec", "", i18n.T("redis.cluster.resize.flag.spec"))
	cmd.Flags().Int32Var(&u.StorageGiB, "storage-gib", 0, i18n.T("redis.cluster.resize.flag.storage_gib"))
	cmd.Flags().StringArrayVar(&params, "param", nil, i18n.T("redis.cluster.resize.flag.param"))
	cmd.Flags().StringVar(&body, "body", "", base.BodyHelpText())
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	return cmd
}
