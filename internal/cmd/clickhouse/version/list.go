// Package version — clickhouse versions.
package version

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

// NewCmd — clickhouse versions.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "versions", Short: i18n.T("clickhouse.version.short")}
	c.AddCommand(base.ListCmd{
		Use: "list", Short: i18n.T("clickhouse.version.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Versions(ctx) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Versions []struct {
					Name string `json:"name"`
				} `json:"versions"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			for _, v := range l.Versions {
				fmt.Fprintln(w, v.Name)
			}

			return nil
		},
	}.Cmd(s))

	return c
}
