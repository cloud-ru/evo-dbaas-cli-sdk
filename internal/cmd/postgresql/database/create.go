package database

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		req  cloud.PostgreSQLDatabaseCreate
		body string
	)

	cmd := &cobra.Command{Use: "create CLUSTER_ID NAME", Short: i18n.T("postgresql.database.create.short"), Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		req.Name = args[1]

		var err error
		if req.Body, err = base.Body(body); err != nil {
			return err
		}

		raw, err := operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.CreateDatabase(cmd.Context(), args[0], req) })
		if err != nil {
			return err
		}

		return s.Printer.Object(raw, func(io.Writer) error {
			s.Printer.Messagef("%s", i18n.T("postgresql.database.created", args[1]))
			return nil
		})
	}}
	cmd.Flags().StringVar(&req.Owner, "owner", "", i18n.T("postgresql.database.flag.owner"))
	cmd.Flags().StringVar(&req.LCCollate, "lc-collate", "", i18n.T("postgresql.flag.lc_collate"))
	cmd.Flags().StringVar(&req.LCCtype, "lc-ctype", "", i18n.T("postgresql.flag.lc_ctype"))
	cmd.Flags().StringVar(&body, "body", "", base.BodyHelpText())
	_ = cmd.MarkFlagRequired("owner")

	return cmd
}
