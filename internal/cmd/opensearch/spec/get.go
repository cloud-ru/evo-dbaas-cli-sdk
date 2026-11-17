package spec

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func getCmd(s *state.State) *cobra.Command {
	cmd := base.ListCmd{Use: "get SPECIFICATION_ID", Short: i18n.T("opensearch.spec.get.short"), Args: cobra.ExactArgs(1), List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
		return operation.With(s, func(h *cloud.OpenSearch) ([]byte, error) { return h.Spec(ctx, args[0]) })
	}}.Cmd(s)

	return cmd
}
