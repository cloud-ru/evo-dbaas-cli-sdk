package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		password base.Password
		roles    []string
		wait     bool
	)

	cmd := &cobra.Command{
		Use:   "create CLUSTER_ID USER_NAME",
		Short: i18n.T("redis.user.create.short"),
		Long:  i18n.T("redis.user.create.long"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := password.Read(cmd, s)
			if err != nil {
				return err
			}

			raw, err := operation.With(s, func(r *cloud.Redis) ([]byte, error) { return r.CreateUser(cmd.Context(), args[0], args[1], pw, roles) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	password.Register(cmd)
	cmd.Flags().StringSliceVar(&roles, "role", nil, i18n.T("redis.user.create.flag.role"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	return cmd
}
