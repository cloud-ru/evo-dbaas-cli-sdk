// Package kafka — cloudru-dbaas kafka: команды Managed Kafka, пакет на ресурс.
package kafka

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/cluster"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/role"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/spec"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/topic"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/user"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/version"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — kafka; defaultURL — значение --api-url по умолчанию. Корень передаёт пустое
// и берёт адрес из конфига при выполнении: файл и профиль — флаги (DESIGN §8.12).
func NewCmd(s *state.State, defaultURL string) *cobra.Command {
	c := &cobra.Command{Use: "kafka", Short: i18n.T("kafka.short")}
	c.PersistentFlags().StringVar(&s.KafkaURL, "api-url", defaultURL, i18n.T("kafka.flag.api_url"))
	c.AddCommand(
		version.NewCmd(s),
		spec.NewCmd(s),
		cluster.NewCmd(s),
		operation.NewCmd(s),
		topic.NewCmd(s),
		user.NewCmd(s),
		role.NewCmd(s),
	)

	return c
}
