// Package redis — cloudru-dbaas redis: команды Managed Redis, пакет на ресурс.
package redis

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/cluster"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/role"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/spec"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/user"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/version"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — redis; defaultURL — значение --api-url по умолчанию. Корень передаёт пустое
// и берёт адрес из конфига при выполнении: файл и профиль — флаги (DESIGN §8.12).
func NewCmd(s *state.State, defaultURL string) *cobra.Command {
	c := &cobra.Command{Use: "redis", Short: i18n.T("redis.short")}
	c.PersistentFlags().StringVar(&s.RedisURL, "api-url", defaultURL, i18n.T("redis.flag.api_url"))
	c.AddCommand(
		version.NewCmd(s),
		spec.NewCmd(s),
		cluster.NewCmd(s),
		operation.NewCmd(s),
		user.NewCmd(s),
		role.NewCmd(s),
	)

	return c
}
