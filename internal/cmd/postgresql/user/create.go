package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		password base.Password
		body     string
	)

	cmd := &cobra.Command{
		Use:   "create CLUSTER_ID USER_NAME",
		Short: i18n.T("postgresql.user.create.short"),
		Long:  i18n.T("postgresql.user.create.long"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := password.Read(cmd, s)
			if err != nil {
				return err
			}

			extra, err := base.Body(body)
			if err != nil {
				return err
			}

			raw, err := operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) {
				return h.CreateUser(cmd.Context(), args[0], args[1], pw, extra)
			})

			return done(s, raw, err, "%s", i18n.T("postgresql.user.created", args[1]))
		},
	}
	password.Register(cmd)
	cmd.Flags().StringVar(&body, "body", "", base.BodyHelpText())

	return cmd
}
