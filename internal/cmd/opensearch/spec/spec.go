// Package spec — opensearch specs.
package spec

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "specs", Aliases: []string{"specifications"}, Short: i18n.T("opensearch.spec.short")}
	c.AddCommand(listCmd(s), getCmd(s))

	return c
}
