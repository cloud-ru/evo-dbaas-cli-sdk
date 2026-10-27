package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func updateCmd(s *state.State) *cobra.Command {
	var (
		password base.Password
		roles    []string
	)

	cmd := &cobra.Command{
		Use:   "update CLUSTER_ID USER_NAME",
		Short: i18n.T("clickhouse.user.update.short"),
		Long:  i18n.T("clickhouse.user.update.long"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var pw *string

			if password.Set() {
				v, err := password.Read(cmd, s)
				if err != nil {
					return err
				}

				pw = &v
			}

			if pw == nil && len(roles) == 0 {
				return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.user.update.err.no_changes")}
			}
			// API ClickHouse: 400 «conflicting update fields: password, granted_roles» (стенд 14.10).
			if pw != nil && len(roles) > 0 {
				return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.user.update.err.conflicting_fields")}
			}

			raw, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) {
				return h.UpdateUser(cmd.Context(), args[0], args[1], pw, roles)
			})

			return done(s, raw, err, "%s", i18n.T("clickhouse.user.updated", args[1]))
		},
	}
	password.RegisterNew(cmd)
	cmd.Flags().StringSliceVar(&roles, "role", nil, i18n.T("clickhouse.user.update.flag.role"))

	return cmd
}
