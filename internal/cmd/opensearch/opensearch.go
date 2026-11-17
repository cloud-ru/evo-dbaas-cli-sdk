// Package opensearch — cloudru-dbaas opensearch: команды Managed OpenSearch, пакет на ресурс.
package opensearch

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/cluster"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/snapshot"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/spec"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/version"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — opensearch; defaultURL — значение --api-url по умолчанию. Корень передаёт пустое
// и берёт адрес из конфига при выполнении: файл и профиль — флаги (DESIGN §8.12).
func NewCmd(s *state.State, defaultURL string) *cobra.Command {
	c := &cobra.Command{Use: "opensearch", Short: i18n.T("opensearch.short")}
	c.PersistentFlags().StringVar(&s.OpenSearchURL, "api-url", defaultURL, i18n.T("opensearch.flag.api_url"))
	c.AddCommand(
		snapshot.NewCmd(s),
		version.NewCmd(s),
		spec.NewCmd(s),
		cluster.NewCmd(s),
		operation.NewCmd(s),
	)

	return c
}
