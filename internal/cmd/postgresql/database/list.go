package database

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

func listCmd(s *state.State) *cobra.Command {
	return base.ListCmd{
		Use: "list CLUSTER_ID", Short: i18n.T("postgresql.database.list.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.Databases(ctx, args[0]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Databases []database `json:"databases"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "NAME", "OWNER", "SIZE_MB", "LC_COLLATE", "LC_CTYPE")
			for _, d := range l.Databases {
				t.Row(d.Name, d.Owner, fmt.Sprint(d.Size), d.LCCollate, d.LCCtype)
			}

			return t.Flush()
		},
	}.Cmd(s)
}
