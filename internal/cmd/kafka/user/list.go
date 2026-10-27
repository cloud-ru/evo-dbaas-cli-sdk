package user

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func listCmd(s *state.State) *cobra.Command {
	return base.ListCmd{
		Use: "list CLUSTER_ID", Short: i18n.T("kafka.user.list.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(r *cloud.Kafka) ([]byte, error) { return r.Users(ctx, args[0]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Users []user `json:"users"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "NAME", "ROLES")
			for _, u := range l.Users {
				t.Row(u.Name, strings.Join(u.roles(), ","))
			}

			return t.Flush()
		},
	}.Cmd(s)
}
