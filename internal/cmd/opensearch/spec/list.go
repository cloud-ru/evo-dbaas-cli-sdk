package spec

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func listCmd(s *state.State) *cobra.Command {
	var f cloud.OpenSearchSpecFilter

	cmd := base.ListCmd{Use: "list", Short: i18n.T("opensearch.spec.list.short"), Args: cobra.NoArgs, List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
		// Живой вызов 16.11: без версии API отвечает 400 «version is required», хотя в спеке она необязательна.
		if f.Version == "" && f.Cluster == "" {
			return nil, &base.ExitError{Code: 1, Msg: i18n.T("opensearch.spec.err.version")}
		}

		return operation.With(s, func(h *cloud.OpenSearch) ([]byte, error) { return h.Specs(ctx, f) })
	}, Table: printSpecs}.Cmd(s)
	cmd.Flags().StringVar(&f.Version, "version", "", i18n.T("opensearch.spec.flag.version"))
	cmd.Flags().StringVar(&f.Zone, "zone", "", i18n.T("opensearch.spec.flag.zone"))
	cmd.Flags().StringVar(&f.Cluster, "cluster-id", "", i18n.T("opensearch.spec.flag.cluster"))
	// cluster_id API принимает только один (живой прогон 17.11, 400) — как у ClickHouse.
	cmd.MarkFlagsMutuallyExclusive("cluster-id", "version")
	cmd.MarkFlagsMutuallyExclusive("cluster-id", "zone")

	return cmd
}

func printSpecs(w io.Writer, raw []byte) error {
	var list struct {
		Specifications []struct {
			ID         string  `json:"id"`
			Name       string  `json:"name"`
			CPU        float64 `json:"data_nodes_cpu"`
			Memory     float64 `json:"data_nodes_memory"`
			MinData    int32   `json:"min_data_nodes"`
			MaxData    int32   `json:"max_data_nodes"`
			MinMaster  int32   `json:"min_master_nodes"`
			MaxMaster  int32   `json:"max_master_nodes"`
			MinDataGiB int32   `json:"min_data_nodes_storage_gigs"`
		} `json:"specifications"`
	}
	if err := output.Decode(raw, &list); err != nil {
		return err
	}

	t := output.NewTable(w, "ID", "NAME", "CPU", "RAM_GB", "DATA_NODES", "MASTER_NODES", "MIN_DATA_GIB")
	for _, sp := range list.Specifications {
		t.Row(sp.ID, sp.Name, fmt.Sprint(sp.CPU), fmt.Sprint(sp.Memory),
			fmt.Sprintf("%d-%d", sp.MinData, sp.MaxData), fmt.Sprintf("%d-%d", sp.MinMaster, sp.MaxMaster), fmt.Sprint(sp.MinDataGiB))
	}

	return t.Flush()
}
