package backup

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
		cluster string
		wait    bool
	)

	cmd := &cobra.Command{Use: "create NAME", Short: i18n.T("postgresql.backup.create.short"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.CreateBackup(cmd.Context(), cluster, args[0]) })
		if err != nil {
			return err
		}

		return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
	}}
	cmd.Flags().StringVar(&cluster, "cluster-id", "", i18n.T("postgresql.backup.flag.cluster"))
	_ = cmd.MarkFlagRequired("cluster-id")
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	return cmd
}
