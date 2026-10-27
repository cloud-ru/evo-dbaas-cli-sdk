// Package role — clickhouse roles.
package role

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — clickhouse roles.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "roles", Short: i18n.T("clickhouse.role.short")}
	c.AddCommand(base.ListCmd{
		Use: "list CLUSTER_ID", Short: i18n.T("clickhouse.role.list.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Roles(ctx, args[0]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Roles []struct {
					Name string `json:"name"`
				} `json:"roles"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			for _, r := range l.Roles {
				fmt.Fprintln(w, r.Name)
			}

			return nil
		},
	}.Cmd(s))

	return c
}
