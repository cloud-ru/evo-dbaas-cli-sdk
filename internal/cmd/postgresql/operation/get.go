package operation

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func getCmd(s *state.State) *cobra.Command {
	var wait bool

	g := &cobra.Command{
		Use: "get OPERATION_ID", Short: i18n.T("postgresql.operation.get.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Первый запрос — без ожидания: опечатка в id даёт 404 и код 1, как
			// без --wait, а не код 8 «операция могла продолжиться».
			raw, err := With(s, func(r *cloud.PostgreSQL) ([]byte, error) { return r.Operation(cmd.Context(), args[0]) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, Ops, GetCmd)
		},
	}
	g.Flags().BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	return g
}

type listed struct {
	Operations []struct {
		ID          string     `json:"id"`
		Done        bool       `json:"done"`
		CreatedAt   *time.Time `json:"created_at"`
		Description string     `json:"description"`
	} `json:"operations"`
}
