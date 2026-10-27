package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		password base.Password
		roles    []string
		wait     bool
	)

	cmd := &cobra.Command{
		Use:   "create CLUSTER_ID USER_NAME",
		Short: i18n.T("kafka.user.create.short"),
		Long:  i18n.T("kafka.user.create.long"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validName(args[1]) {
				return &base.ExitError{Code: 1, Msg: i18n.T("kafka.user.err.name", args[1])}
			}

			pw, err := password.Read(cmd, s)
			if err != nil {
				return err
			}

			raw, err := operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.CreateUser(cmd.Context(), args[0], args[1], pw, roles) })
			if err != nil {
				return err
			}

			return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
		},
	}
	password.Register(cmd)
	cmd.Flags().StringSliceVar(&roles, "role", nil, i18n.T("kafka.user.create.flag.role"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("kafka.cluster.flag.wait"))

	return cmd
}

// validName — правило API Kafka (400 в прогоне 14.10): только a–z и 0–9.
func validName(n string) bool {
	if n == "" {
		return false
	}

	for _, c := range n {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}

	return true
}
