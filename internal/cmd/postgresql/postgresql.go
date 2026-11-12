// Package postgresql — cloudru-dbaas postgresql: команды Managed PostgreSQL, пакет на ресурс.
package postgresql

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/backup"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/cluster"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/database"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/spec"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/user"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/version"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — postgresql; defaultURL — значение --api-url по умолчанию. Корень передаёт пустое
// и берёт адрес из конфига при выполнении: файл и профиль — флаги (DESIGN §8.12).
func NewCmd(s *state.State, defaultURL string) *cobra.Command {
	c := &cobra.Command{Use: "postgresql", Short: i18n.T("postgresql.short")}
	c.PersistentFlags().StringVar(&s.PostgreSQLURL, "api-url", defaultURL, i18n.T("postgresql.flag.api_url"))
	c.AddCommand(
		backup.NewCmd(s),
		version.NewCmd(s),
		spec.NewCmd(s),
		cluster.NewCmd(s),
		operation.NewCmd(s),
		database.NewCmd(s),
		user.NewCmd(s),
	)

	return c
}
