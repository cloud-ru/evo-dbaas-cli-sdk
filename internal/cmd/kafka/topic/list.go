package topic

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func listCmd(s *state.State) *cobra.Command {
	var filter string

	cmd := base.ListCmd{
		Use: "list CLUSTER_ID", Short: i18n.T("kafka.topic.list.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.Topics(ctx, args[0], filter) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Topics []topic `json:"topics"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "NAME", "PARTITIONS", "REPLICATION")
			for _, tp := range l.Topics {
				t.Row(tp.Name, fmt.Sprint(tp.NumPartitions), fmt.Sprint(tp.ReplicationFactor))
			}

			return t.Flush()
		},
	}.Cmd(s)
	cmd.Flags().StringVar(&filter, "filter", "", i18n.T("kafka.topic.list.flag.filter"))

	return cmd
}
