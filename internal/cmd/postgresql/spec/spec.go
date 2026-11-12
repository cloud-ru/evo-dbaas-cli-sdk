// Package spec — postgresql specs.
package spec

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func NewCmd(s *state.State) *cobra.Command {
	var f cloud.PostgreSQLSpecFilter

	list := base.ListCmd{Use: "list", Short: i18n.T("postgresql.spec.list.short"), Args: cobra.NoArgs, List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
		// API отвечает 404 на список без версии, хотя в спеке version_name необязателен.
		if f.Version == "" && f.Cluster == "" {
			return nil, &base.ExitError{Code: 1, Msg: i18n.T("postgresql.spec.err.version")}
		}

		return operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.Specs(ctx, f) })
	}, Table: func(w io.Writer, raw []byte) error {
		var list struct {
			Specifications []struct {
				ID       string  `json:"id"`
				Name     string  `json:"display_name"`
				CPU      float64 `json:"cpu"`
				Memory   float64 `json:"memory"`
				Mode     string  `json:"deployment_mode"`
				MaxHosts int32   `json:"max_hosts"`
				Storage  int32   `json:"min_storage_gb"`
			} `json:"specifications"`
		}
		if err := output.Decode(raw, &list); err != nil {
			return err
		}

		t := output.NewTable(w, "ID", "NAME", "CPU", "RAM_GB", "MODE", "MAX_HOSTS", "MIN_STORAGE")
		for _, sp := range list.Specifications {
			t.Row(sp.ID, sp.Name, fmt.Sprint(sp.CPU), fmt.Sprint(sp.Memory), sp.Mode, fmt.Sprint(sp.MaxHosts), fmt.Sprint(sp.Storage))
		}

		return t.Flush()
	}}.Cmd(s)
	list.Flags().StringVar(&f.Version, "version", "", i18n.T("postgresql.spec.flag.version"))
	list.Flags().StringVar(&f.Cluster, "cluster-id", "", i18n.T("postgresql.spec.flag.cluster_id"))
	list.MarkFlagsMutuallyExclusive("cluster-id", "version")

	c := &cobra.Command{Use: "specs", Aliases: []string{"specifications"}, Short: i18n.T("postgresql.spec.short")}
	c.AddCommand(list)

	return c
}
