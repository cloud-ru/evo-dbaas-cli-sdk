// Package spec — kafka specs.
package spec

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

type spec struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	CPU            *float64 `json:"cpu"`
	Memory         *float64 `json:"memory"`
	MinBrokers     *int32   `json:"min_brokers"`
	MaxBrokers     *int32   `json:"max_brokers"`
	MinStorageGigs *int32   `json:"min_storage_gigs"`
}

// NewCmd — kafka specs.
func NewCmd(s *state.State) *cobra.Command {
	var f cloud.KafkaSpecFilter

	list := base.ListCmd{
		Use: "list", Short: i18n.T("kafka.spec.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			q := f
			if q.Cluster == "" && (q.Version == "" || q.Zone == "") {
				return nil, &base.ExitError{Code: 1, Msg: i18n.T("kafka.spec.err.filters_required")}
			}

			if q.Zone != "" {
				zid, err := base.ZoneID(ctx, s, q.Zone)
				if err != nil {
					return nil, err
				}

				q.Zone = zid
			}

			return operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.Specs(ctx, q) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Specifications []spec `json:"specifications"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "ID", "NAME", "CPU", "RAM_GB", "BROKERS", "MIN_STORAGE")
			for _, sp := range l.Specifications {
				t.Row(sp.ID, sp.Name, num(sp.CPU), num(sp.Memory), i32(sp.MinBrokers)+"–"+i32(sp.MaxBrokers), i32(sp.MinStorageGigs))
			}

			return t.Flush()
		},
	}.Cmd(s)
	// Без версии и зоны API отвечает 400, хотя в спецификации оба параметра необязательны.
	list.Flags().StringVar(&f.Version, "version-id", "", i18n.T("kafka.cluster.create.flag.version_id"))
	list.Flags().StringVar(&f.Zone, "zone", "", i18n.T("kafka.spec.flag.zone"))
	list.Flags().StringVar(&f.Cluster, "cluster-id", "", i18n.T("kafka.spec.flag.cluster_id"))

	c := &cobra.Command{Use: "specs", Aliases: []string{"specifications"}, Short: i18n.T("kafka.spec.short")}
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
