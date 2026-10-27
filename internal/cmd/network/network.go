// Package network — cloudru-dbaas network: подсети (API виртуальных машин) и
// VPC (Evolution VPC API). Кластеру ClickHouse, Redis и Kafka нужна подсеть;
// в новом проекте есть VPC Default и подсеть Default_<зона>, но не во всех зонах.
package network

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — network.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "network", Short: i18n.T("network.short")}
	c.AddCommand(subnetsCmd(s), vpcsCmd(s))

	return c
}
