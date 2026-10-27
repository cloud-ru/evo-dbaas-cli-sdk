// Package version — redis versions.
package version

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — redis versions.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "versions", Short: i18n.T("redis.version.short")}
	c.AddCommand(base.ListCmd{
		Use: "list", Short: i18n.T("redis.version.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			return operation.With(s, func(r *cloud.Redis) ([]byte, error) { return r.Versions(ctx) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Versions []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"versions"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "ID", "NAME")
			for _, v := range l.Versions {
				t.Row(v.ID, v.Name)
			}

			return t.Flush()
		},
	}.Cmd(s))

	return c
}
