package cluster

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
	return base.ListCmd{
		Use: "list", Short: i18n.T("kafka.cluster.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			project, err := s.NeedProject()
			if err != nil {
				return nil, err
			}

			return operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.Clusters(ctx, project) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Clusters []cluster `json:"clusters"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "ID", "NAME", "STATUS", "HEALTH", "BROKERS", "STORAGE_GIB")
			for _, cl := range l.Clusters {
				t.Row(cl.ID, cl.Name, cl.Status, cl.Health, fmt.Sprint(cl.Brokers), fmt.Sprint(cl.StorageGiB))
			}

			return t.Flush()
		},
	}.Cmd(s)
}
