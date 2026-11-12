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

func describeCmd(s *state.State) *cobra.Command {
	return base.DescribeCmd{
		Use: "get CLUSTER_ID NAME", Short: i18n.T("postgresql.database.get.short"), Args: cobra.ExactArgs(2),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.Database(ctx, args[0], args[1]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var d database
			if err := output.Decode(raw, &d); err != nil {
				return err
			}

			fmt.Fprintf(w, "%s\t%s\n", d.Name, d.Owner)

			return nil
		},
	}.Cmd(s)
}
