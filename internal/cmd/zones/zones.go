// Package zones — cloudru-dbaas zones: зоны доступности из API виртуальных машин.
package zones

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — zones.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "zones", Short: i18n.T("zones.short")}
	c.AddCommand(base.ListCmd{
		Use: "list", Short: i18n.T("zones.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			c, err := s.Compute()
			if err != nil {
				return nil, err
			}

			raw, err := c.Zones(ctx)
			if err != nil {
				return nil, i18n.Errorf("zones.err.list", err)
			}

			var zs []zone
			if err := output.Decode(raw, &zs); err != nil {
				return nil, &base.ExitError{Code: 1, Msg: i18n.T("zones.err.parse", err)}
			}

			return raw, nil
		},
		Table: func(w io.Writer, raw []byte) error {
			var zs []zone

			_ = output.Decode(raw, &zs)

			t := output.NewTable(w, "ID", "NAME", "DEFAULT")
			for _, z := range zs {
				t.Row(z.ID, z.Name, fmt.Sprint(z.Default))
			}

			return t.Flush()
		},
	}.Cmd(s))

	return c
}

type zone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}
