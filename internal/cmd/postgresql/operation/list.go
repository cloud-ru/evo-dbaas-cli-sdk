package operation

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func listCmd(s *state.State) *cobra.Command {
	return base.ListCmd{
		Use: "list CLUSTER_ID", Short: i18n.T("postgresql.operation.list.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return With(s, func(r *cloud.PostgreSQL) ([]byte, error) { return r.ClusterOperations(ctx, args[0]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l listed
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "ID", "DONE", "CREATED", "DESCRIPTION")
			for _, op := range l.Operations {
				created := ""
				if op.CreatedAt != nil {
					created = op.CreatedAt.Format(time.RFC3339)
				}

				t.Row(op.ID, fmt.Sprint(op.Done), created, op.Description)
			}

			return t.Flush()
		},
	}.Cmd(s)
}
