// Package cluster — postgresql clusters: файл на глагол.
package cluster

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "clusters", Aliases: []string{"cluster"}, Short: i18n.T("postgresql.cluster.short")}
	c.AddCommand(listCmd(s), describeCmd(s), createCmd(s), updateCmd(s), deleteCmd(s), suspendCmd(s), resumeCmd(s), switchoverCmd(s), storageinfoCmd(s), localesCmd(s))

	return c
}

type cluster struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Health    string   `json:"health"`
	Version   string   `json:"version"`
	Spec      string   `json:"specification_id"`
	Mode      string   `json:"deployment_mode"`
	Instances int32    `json:"instances"`
	Subnets   []string `json:"subnet_ids"`
	Zones     []string `json:"availability_zone_ids"`
	Storage   struct {
		Data int32 `json:"pg_data_gb"`
		WAL  int32 `json:"pg_wal_gb"`
	} `json:"storage"`
}

// clusterModes загружает справочник по разу для каждой версии; исходный JSON не меняет.
func clusterModes(ctx context.Context, s *state.State, clusters []cluster) error {
	h, err := s.PostgreSQL()
	if err != nil {
		return err
	}

	modes := map[string]map[string]string{}
	for i, cl := range clusters {
		if cl.Mode != "" {
			continue
		}

		if _, ok := modes[cl.Version]; !ok {
			modes[cl.Version] = map[string]string{}

			specs, err := loadSpecs(ctx, h, cl.Version)
			if err != nil {
				s.Printer.Messagef("%s", i18n.T("postgresql.cluster.warning.specs", err))
			} else {
				for _, sp := range specs {
					modes[cl.Version][sp.ID] = sp.Mode
				}
			}
		}

		clusters[i].Mode = modes[cl.Version][cl.Spec]
	}

	return nil
}

func printCluster(ctx context.Context, s *state.State, w io.Writer, raw []byte) error {
	var cl cluster
	if err := output.Decode(raw, &cl); err != nil {
		return err
	}

	clusters := []cluster{cl}
	if err := clusterModes(ctx, s, clusters); err != nil {
		return err
	}

	cl = clusters[0]
	t := output.NewTable(w)
	t.Field("id", cl.ID)
	t.Field("name", cl.Name)
	t.Field("status", cl.Status)
	t.Field("health", cl.Health)
	t.Field("version", cl.Version)
	t.Field("specification_id", cl.Spec)
	t.Field("deployment_mode", cl.Mode)
	t.Field("instances", fmt.Sprint(cl.Instances))
	t.Field("subnet_ids", strings.Join(cl.Subnets, ", "))

	if len(cl.Zones) > 0 {
		t.Field("availability_zone_ids", strings.Join(cl.Zones, ", "))
	}

	t.Field("pg_data_gb", fmt.Sprint(cl.Storage.Data))

	if cl.Storage.WAL > 0 {
		t.Field("pg_wal_gb", fmt.Sprint(cl.Storage.WAL))
	}

	return t.Flush()
}
