package cluster

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func listCmd(s *state.State) *cobra.Command {
	return base.ListCmd{
		Use: "list", Short: i18n.T("opensearch.cluster.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			project, err := s.NeedProject()
			if err != nil {
				return nil, err
			}

			return operation.With(s, func(h *cloud.OpenSearch) ([]byte, error) { return h.Clusters(ctx, project) })
		}, Table: func(w io.Writer, raw []byte) error {
			var list struct {
				Clusters []cluster `json:"clusters"`
			}
			if err := output.Decode(raw, &list); err != nil {
				return err
			}

			t := output.NewTable(w, "ID", "NAME", "STATUS", "HEALTH", "VERSION", "DATA_NODES", "MASTER_NODES", "DATA_SUBNET_IDS", "MASTER_SUBNET_IDS")
			for _, cl := range list.Clusters {
				t.Row(cl.ID, cl.Name, cl.Status, cl.Health, cl.Version, fmt.Sprint(cl.DataNodes), fmt.Sprint(cl.MasterNodes), strings.Join(cl.DataSubnets, ","), strings.Join(cl.MasterSubnets, ","))
			}

			return t.Flush()
		},
	}.Cmd(s)
}
