package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
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
		Short: i18n.T("redis.user.update.short"),
		Long:  i18n.T("redis.user.update.long"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !password.Set() && len(roles) == 0 {
				return &base.ExitError{Code: 1, Msg: i18n.T("redis.user.update.err.no_changes")}
			}

			var pw *string

			if password.Set() {
				v, err := password.Read(cmd, s)
				if err != nil {
					return err
				}

				pw = &v
			}

			if len(roles) == 0 {
				// PUT заменяет пользователя целиком: без --role переносим текущие роли,
				// чтобы смена пароля не обнулила права.
				raw, err := operation.With(s, func(r *cloud.Redis) ([]byte, error) { return r.User(cmd.Context(), args[0], args[1]) })
				if err != nil {
					return err
				}

				var u user
				if err := output.Decode(raw, &u); err != nil {
					return err
				}

				roles = u.roles()
			}

			raw, err := operation.With(s, func(r *cloud.Redis) ([]byte, error) { return r.UpdateUser(cmd.Context(), args[0], args[1], pw, roles) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	password.RegisterNew(cmd)
	cmd.Flags().StringSliceVar(&roles, "role", nil, i18n.T("redis.user.update.flag.role"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	return cmd
}
