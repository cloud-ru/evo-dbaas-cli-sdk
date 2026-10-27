// Package cluster — redis clusters: файл на глагол.
package cluster

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — redis clusters.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "clusters", Aliases: []string{"cluster"}, Short: i18n.T("redis.cluster.short")}
	c.AddCommand(listCmd(s), describeCmd(s), createCmd(s), resizeCmd(s), deleteCmd(s))

	return c
}

// cluster — поля кластера для человека; --json печатает ответ как есть.
type cluster struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	Health          string     `json:"health"`
	Type            string     `json:"type"`
	VersionID       string     `json:"version_id"`
	SpecificationID string     `json:"specification_id"`
	Shards          int32      `json:"shards"`
	Replicas        int32      `json:"replicas"`
	StorageGiB      *int32     `json:"storage_gib"`
	Hosts           *[]string  `json:"redis_hosts"`
	Subnets         *[]string  `json:"subnet_ids"`
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
	kv("type", cl.Type)
	kv("version_id", cl.VersionID)
	kv("specification_id", cl.SpecificationID)
	kv("shards", fmt.Sprint(cl.Shards))
	kv("replicas", fmt.Sprint(cl.Replicas))
	kv("storage_gib", i32(cl.StorageGiB))

	if cl.Hosts != nil {
		kv("hosts", strings.Join(*cl.Hosts, ", "))
	}

	if cl.Subnets != nil {
		kv("subnet_ids", strings.Join(*cl.Subnets, ", "))
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
