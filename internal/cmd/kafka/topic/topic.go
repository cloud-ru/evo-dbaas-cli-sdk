// Package topic — kafka topics: файл на глагол.
package topic

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — kafka topics.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "topics", Aliases: []string{"topic"}, Short: i18n.T("kafka.topic.short")}
	c.AddCommand(listCmd(s), createCmd(s), deleteCmd(s), describeCmd(s), updateCmd(s))

	return c
}

// topic — поля топика для человека; --json печатает ответ как есть.
type topic struct {
	Name              string            `json:"name"`
	NumPartitions     int32             `json:"num_partitions"`
	ReplicationFactor int32             `json:"replication_factor"`
	Parameters        map[string]string `json:"parameters"`
}
