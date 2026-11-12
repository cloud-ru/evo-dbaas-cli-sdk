package cluster

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func describeCmd(s *state.State) *cobra.Command {
	return &cobra.Command{Use: "get CLUSTER_ID", Short: i18n.T("postgresql.cluster.get.short"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		h, err := s.PostgreSQL()
		if err != nil {
			return err
		}

		raw, err := h.Cluster(cmd.Context(), args[0])
		if err != nil {
			return err
		}

		return s.Printer.Object(raw, func(w io.Writer) error { return printCluster(cmd.Context(), s, w, raw) })
	}}
}
