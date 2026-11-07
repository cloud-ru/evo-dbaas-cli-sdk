// Package spec — clickhouse specs.
package spec

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

type spec struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	CPU            *float64 `json:"cpu"`
	Memory         *float64 `json:"memory"`
	ClusterType    string   `json:"cluster_type"`
	MinShards      *int32   `json:"min_shards"`
	MaxShards      *int32   `json:"max_shards"`
	MinReplicas    *int32   `json:"min_replicas"`
	MaxReplicas    *int32   `json:"max_replicas"`
	MinStorageGigs *int32   `json:"min_storage_gigs"`
	Role           string   `json:"role"`
}

// NewCmd — clickhouse specs.
func NewCmd(s *state.State) *cobra.Command {
	var f cloud.ClickHouseSpecFilter

	list := base.ListCmd{
		Use: "list", Short: i18n.T("clickhouse.spec.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			// Без зоны и cluster_id API отвечает 400 «AvailabilityZoneId: value must be a valid UUID» (стенд 14.10).
			q := f
			if q.Zone == "" && q.Cluster == "" {
				return nil, &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.spec.err.zone_required")}
			}

			var err error
			if q.Zone != "" {
				if q.Zone, err = base.ZoneID(ctx, s, q.Zone); err != nil {
					return nil, err
				}
			}

			if q.Role != "" {
				if q.Role, err = base.OneOf("role", q.Role, "server", "keeper"); err != nil {
					return nil, err
				}
			}

			return operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Specs(ctx, q) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Specifications []spec `json:"specifications"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "ID", "NAME", "CPU", "RAM_GB", "TYPE", "SHARDS", "REPLICAS", "MIN_STORAGE", "ROLE")
			for _, sp := range l.Specifications {
				t.Row(sp.ID, sp.Name, num(sp.CPU), num(sp.Memory), sp.ClusterType,
					i32(sp.MinShards)+"–"+i32(sp.MaxShards), i32(sp.MinReplicas)+"–"+i32(sp.MaxReplicas),
					i32(sp.MinStorageGigs), strings.TrimPrefix(sp.Role, "SPECIFICATION_ROLE_"))
			}

			return t.Flush()
		},
	}.Cmd(s)
	list.Flags().StringVar(&f.Cluster, "cluster-id", "", i18n.T("clickhouse.spec.flag.cluster_id"))
	list.Flags().StringVar(&f.Version, "version", "", i18n.T("clickhouse.spec.flag.version"))
	list.Flags().StringVar(&f.Zone, "zone", "", i18n.T("clickhouse.spec.flag.zone"))
	list.Flags().StringVar(&f.Role, "role", "server", i18n.T("clickhouse.spec.flag.role"))
	// cluster_id вместе с version или availability_zone_id API отклоняет: 400 «cannot be specified together» (живой прогон 7.11).
	list.MarkFlagsMutuallyExclusive("cluster-id", "version")
	list.MarkFlagsMutuallyExclusive("cluster-id", "zone")

	c := &cobra.Command{Use: "specs", Aliases: []string{"specifications"}, Short: i18n.T("clickhouse.spec.short")}
	c.AddCommand(list)

	return c
}

func i32(p *int32) string {
	if p == nil {
		return ""
	}

	return fmt.Sprint(*p)
}

func num(p *float64) string {
	if p == nil {
		return ""
	}

	return fmt.Sprintf("%g", *p)
}
