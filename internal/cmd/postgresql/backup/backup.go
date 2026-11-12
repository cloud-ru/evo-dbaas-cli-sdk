// Package backup — PostgreSQL backups.
package backup

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "backups", Short: i18n.T("postgresql.backup.short")}
	c.AddCommand(listCmd(s), getCmd(s), createCmd(s))

	return c
}
