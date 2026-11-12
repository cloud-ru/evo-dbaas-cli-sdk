package cluster

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func resumeCmd(s *state.State) *cobra.Command {
	return base.DeleteCmd{
		Use: "resume CLUSTER_ID", Short: i18n.T("postgresql.cluster.resume.short"), Args: cobra.ExactArgs(1),
		Delete: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.PostgreSQL) ([]byte, error) { return h.ResumeCluster(ctx, args[0]) })
		},
		Ops: operation.Ops, OpsCmd: operation.GetCmd,
		NeedYesKey: func() string { return i18n.T("postgresql.cluster.resume.err.need_yes") },
		YesFlagKey: func() string { return i18n.T("postgresql.cluster.resume.flag.yes") },
	}.Cmd(s)
}
