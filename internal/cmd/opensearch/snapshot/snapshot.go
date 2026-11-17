// Package snapshot — OpenSearch snapshots.
package snapshot

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "snapshots", Short: i18n.T("opensearch.snapshot.short")}
	c.AddCommand(listCmd(s), getCmd(s), createCmd(s), deleteCmd(s))

	return c
}
