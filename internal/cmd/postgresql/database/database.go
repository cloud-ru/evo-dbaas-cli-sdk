package database

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

type database struct {
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	Size      uint32 `json:"size_mb"`
	LCCollate string `json:"lc_collate"`
	LCCtype   string `json:"lc_ctype"`
}

func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "databases", Aliases: []string{"db"}, Short: i18n.T("postgresql.database.short")}
	c.AddCommand(listCmd(s), describeCmd(s), createCmd(s), deleteCmd(s))

	return c
}
