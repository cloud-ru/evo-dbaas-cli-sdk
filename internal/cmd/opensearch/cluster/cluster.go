// Package cluster — opensearch clusters: файл на глагол.
package cluster

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "clusters", Aliases: []string{"cluster"}, Short: i18n.T("opensearch.cluster.short")}
	c.AddCommand(listCmd(s), describeCmd(s), createCmd(s), updateCmd(s), deleteCmd(s))

	return c
}

type cluster struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	Health        string   `json:"health"`
	Version       string   `json:"version"`
	Spec          string   `json:"specification_id"`
	DataNodes     int32    `json:"data_nodes"`
	MasterNodes   int32    `json:"master_nodes"`
	DataGiB       int32    `json:"data_nodes_disk_size_gib"`
	MasterGiB     int32    `json:"master_nodes_disk_size_gib"`
	DataSubnets   []string `json:"data_nodes_subnet_ids"`
	MasterSubnets []string `json:"master_nodes_subnet_ids"`
	Hosts         []string `json:"hosts"`
}

func printCluster(w io.Writer, raw []byte) error {
	var cl cluster
	if err := output.Decode(raw, &cl); err != nil {
		return err
	}

	t := output.NewTable(w)
	t.Field("id", cl.ID)
	t.Field("name", cl.Name)
	t.Field("status", cl.Status)
	t.Field("health", cl.Health)
	t.Field("version", cl.Version)
	t.Field("specification_id", cl.Spec)
	t.Field("data_nodes", fmt.Sprint(cl.DataNodes))
	t.Field("master_nodes", fmt.Sprint(cl.MasterNodes))
	t.Field("data_nodes_disk_size_gib", fmt.Sprint(cl.DataGiB))
	t.Field("master_nodes_disk_size_gib", fmt.Sprint(cl.MasterGiB))
	t.Field("data_nodes_subnet_ids", strings.Join(cl.DataSubnets, ", "))
	t.Field("master_nodes_subnet_ids", strings.Join(cl.MasterSubnets, ", "))
	t.Field("hosts", strings.Join(cl.Hosts, ", "))

	return t.Flush()
}
