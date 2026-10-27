// Package clickhouse — cloudru-dbaas clickhouse: команды Managed ClickHouse, пакет на ресурс.
package clickhouse

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/auth"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/cluster"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/database"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/role"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/spec"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/user"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/version"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — clickhouse; defaultURL — адрес API по умолчанию из конфига.
func NewCmd(s *state.State, defaultURL string) *cobra.Command {
	c := &cobra.Command{Use: "clickhouse", Aliases: []string{"ch"}, Short: i18n.T("clickhouse.short")}
	c.PersistentFlags().StringVar(&s.ClickHouseURL, "api-url", defaultURL, i18n.T("clickhouse.flag.api_url"))
	c.AddCommand(
		auth.NewCmd(s),
		version.NewCmd(s),
		spec.NewCmd(s),
		cluster.NewCmd(s),
		operation.NewCmd(s),
		database.NewCmd(s),
		user.NewCmd(s),
		role.NewCmd(s),
	)

	return c
}
