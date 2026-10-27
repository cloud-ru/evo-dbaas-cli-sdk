package user

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
		password base.Password
		roles    []string
		wait     bool
	)

	cmd := &cobra.Command{
		Use:   "update CLUSTER_ID USER_NAME",
		Short: i18n.T("kafka.user.update.short"),
		Long:  i18n.T("kafka.user.update.long"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !password.Set() && len(roles) == 0 {
				return &base.ExitError{Code: 1, Msg: i18n.T("kafka.user.update.err.no_changes")}
			}

			var pw *string

			if password.Set() {
				v, err := password.Read(cmd, s)
				if err != nil {
					return err
				}

				pw = &v
			}

			raw, err := operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.UpdateUser(cmd.Context(), args[0], args[1], pw, roles) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	password.RegisterNew(cmd)
	cmd.Flags().StringSliceVar(&roles, "role", nil, i18n.T("kafka.user.update.flag.role"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("kafka.cluster.flag.wait"))

	return cmd
}
