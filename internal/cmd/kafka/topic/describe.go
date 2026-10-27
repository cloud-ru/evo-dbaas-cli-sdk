package topic

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"

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
		Use: "get CLUSTER_ID TOPIC", Short: i18n.T("kafka.topic.get.short"), Args: cobra.ExactArgs(2),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.Topic(ctx, args[0], args[1]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var t topic
			if err := output.Decode(raw, &t); err != nil {
				return err
			}

			fmt.Fprintf(w, "%s\n", i18n.T("kafka.topic.summary", t.Name, t.NumPartitions, t.ReplicationFactor))

			for _, k := range slices.Sorted(maps.Keys(t.Parameters)) {
				fmt.Fprintf(w, "  %s=%s\n", k, t.Parameters[k])
			}

			return nil
		},
	}.Cmd(s)
}
