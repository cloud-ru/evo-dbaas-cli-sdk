package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func updateCmd(s *state.State) *cobra.Command {
	var (
		password base.Password
		roles    []string
		body     string
	)

	cmd := &cobra.Command{
		Use:   "update CLUSTER_ID USER_NAME",
		Short: i18n.T("postgresql.user.update.short"),
		Long:  i18n.T("postgresql.user.update.long"),
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

			if pw == nil && roles == nil && body == "" {
				return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.user.update.err.no_changes")}
			}

			extra, err := base.Body(body)
			if err != nil {
				return err
			}

			raw, err := operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) {
				return h.UpdateUser(cmd.Context(), args[0], args[1], pw, roles, extra)
			})

			return done(s, raw, err, "%s", i18n.T("postgresql.user.updated", args[1]))
		},
	}
	cmd.Flags().StringVar(&body, "body", "", base.BodyHelpText())
	password.RegisterNew(cmd)
	cmd.Flags().StringSliceVar(&roles, "role", nil, i18n.T("postgresql.user.update.flag.role"))

	return cmd
}
