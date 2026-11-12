package cluster

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func listCmd(s *state.State) *cobra.Command {
	return &cobra.Command{Use: "list", Short: i18n.T("postgresql.cluster.list.short"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		project, err := s.NeedProject()
		if err != nil {
			return err
		}

		h, err := s.PostgreSQL()
		if err != nil {
			return err
		}

		raw, err := h.Clusters(cmd.Context(), project)
		if err != nil {
			return err
		}

		return s.Printer.Object(raw, func(w io.Writer) error {
			var list struct {
				Clusters []cluster `json:"clusters"`
			}
			if err := output.Decode(raw, &list); err != nil {
				return err
			}

			if err := clusterModes(cmd.Context(), s, list.Clusters); err != nil {
				return err
			}

			t := output.NewTable(w, "ID", "NAME", "STATUS", "HEALTH", "VERSION", "MODE", "INSTANCES", "SUBNET_IDS", "ZONES")
			for _, cl := range list.Clusters {
				t.Row(cl.ID, cl.Name, cl.Status, cl.Health, cl.Version, cl.Mode, fmt.Sprint(cl.Instances), strings.Join(cl.Subnets, ","), strings.Join(cl.Zones, ","))
			}

			return t.Flush()
		})
	}}
}
