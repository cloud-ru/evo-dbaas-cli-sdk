package user

import (
	"context"
	"fmt"
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

func describeCmd(s *state.State) *cobra.Command {
	return base.DescribeCmd{
		Use: "get CLUSTER_ID USER_NAME", Short: i18n.T("kafka.user.get.short"), Args: cobra.ExactArgs(2),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(r *cloud.Kafka) ([]byte, error) { return r.User(ctx, args[0], args[1]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var u user
			if err := output.Decode(raw, &u); err != nil {
				return err
			}

			fmt.Fprintf(w, "%s\t%s\n", u.Name, strings.Join(u.roles(), ","))

			return nil
		},
	}.Cmd(s)
}
