package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func deleteCmd(s *state.State) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use: "delete CLUSTER_ID USER_NAME", Short: i18n.T("postgresql.user.delete.short"), Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return &base.ExitError{Code: 1, Msg: i18n.T("common.err.need_yes")}
			}

			raw, err := operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.DeleteUser(cmd.Context(), args[0], args[1]) })

			return done(s, raw, err, "%s", i18n.T("postgresql.user.deleted", args[1]))
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, i18n.T("common.flag.yes"))

	return cmd
}

// done печатает ответ API как есть, человеку — сообщение: у пользователей
// PostgreSQL ответ — не операция.
