// Package cluster — clickhouse clusters: файл на глагол.
package cluster

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — clickhouse clusters.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "clusters", Aliases: []string{"cluster"}, Short: i18n.T("clickhouse.cluster.short")}
	c.AddCommand(listCmd(s), describeCmd(s), createCmd(s), resizeCmd(s), deleteCmd(s))

	return c
}

// cluster — поля кластера для человека; --json печатает ответ как есть.
type cluster struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	Health          string     `json:"health"`
	Version         string     `json:"version"`
	SpecificationID string     `json:"specification_id"`
	Shards          int32      `json:"shards"`
	Replicas        int32      `json:"replicas"`
	StorageGiB      *int32     `json:"storage_gib"`
	Hosts           *[]string  `json:"hosts"`
	SubnetIDs       *[]string  `json:"subnet_ids"`
	CreatedAt       *time.Time `json:"created_at"`
}

func printCluster(w io.Writer, raw []byte) error {
	var cl cluster
	if err := output.Decode(raw, &cl); err != nil {
		return err
	}

	t := output.NewTable(w)
	kv := t.Field
	kv("id", cl.ID)
	kv("name", cl.Name)
	kv("status", cl.Status)
	kv("health", cl.Health)
	kv("version", cl.Version)
	kv("specification_id", cl.SpecificationID)
	kv("shards", fmt.Sprint(cl.Shards))
	kv("replicas", i18n.T("clickhouse.cluster.replicas", cl.Replicas))
	kv("storage_gib", i32(cl.StorageGiB))

	if cl.Hosts != nil {
		kv("hosts", strings.Join(*cl.Hosts, ", "))
	}

	if cl.SubnetIDs != nil {
		kv("subnet_ids", strings.Join(*cl.SubnetIDs, ", "))
	}

	if cl.CreatedAt != nil {
		kv("created_at", cl.CreatedAt.Format(time.RFC3339))
	}

	return t.Flush()
}

func i32(p *int32) string {
	if p == nil {
		return ""
	}

	return fmt.Sprint(*p)
}

// keeperFlags — ресурсы выделенного ClickHouse Keeper (standalone_keeper).
func keeperFlags(cmd *cobra.Command, k *cloud.ClickHouseKeeper) {
	cmd.Flags().StringVar(&k.Spec, "keeper-spec", "", i18n.T("clickhouse.cluster.flag.keeper_spec"))
	cmd.Flags().Int32Var(&k.StorageGiB, "keeper-storage-gib", 0, i18n.T("clickhouse.cluster.flag.keeper_storage_gib"))
}
