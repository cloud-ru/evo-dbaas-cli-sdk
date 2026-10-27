package network

import (
	"context"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func vpcsCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "vpcs", Short: i18n.T("network.vpc.short")}
	c.AddCommand(base.ListCmd{
		Use: "list", Short: i18n.T("network.vpc.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			project, err := s.NeedProject()
			if err != nil {
				return nil, err
			}

			return withVPC(s, func(v *cloud.VPC) ([]byte, error) { return v.List(ctx, project) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var r struct {
				Vpcs []struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Default bool   `json:"default"`
				} `json:"vpcs"`
			}

			_ = output.Decode(raw, &r)

			t := output.NewTable(w, "ID", "NAME", "DEFAULT")
			for _, v := range r.Vpcs {
				t.Row(v.ID, v.Name, strconv.FormatBool(v.Default))
			}

			return t.Flush()
		},
	}.Cmd(s), base.DescribeCmd{
		Use: "get VPC_ID", Short: i18n.T("network.vpc.get.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return withVPC(s, func(v *cloud.VPC) ([]byte, error) { return v.Get(ctx, args[0]) })
		},
	}.Cmd(s), vpcCreateCmd(s), vpcDeleteCmd(s))

	return c
}

func vpcCreateCmd(s *state.State) *cobra.Command {
	var (
		req  cloud.VPCCreate
		wait bool
	)

	cmd := &cobra.Command{
		Use: "create", Short: i18n.T("network.vpc.create.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			if req.Project, err = s.NeedProject(); err != nil {
				return err
			}

			raw, err := withVPC(s, func(v *cloud.VPC) ([]byte, error) { return v.Create(cmd.Context(), req) })
			if err != nil {
				return err
			}

			return finishVPCOp(cmd, s, raw, wait)
		},
	}
	cmd.Flags().StringVar(&req.Name, "name", "", i18n.T("common.flag.name"))
	cmd.Flags().StringVar(&req.Description, "description", "", i18n.T("common.flag.description"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("common.flag.wait_op"))
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func vpcDeleteCmd(s *state.State) *cobra.Command {
	var yes, wait bool

	cmd := &cobra.Command{
		Use: "delete VPC_ID", Short: i18n.T("network.vpc.delete.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return &base.ExitError{Code: 1, Msg: i18n.T("common.err.need_yes")}
			}

			raw, err := withVPC(s, func(v *cloud.VPC) ([]byte, error) { return v.Delete(cmd.Context(), args[0]) })
			if err != nil {
				return err
			}

			return finishVPCOp(cmd, s, raw, wait)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, i18n.T("common.flag.yes"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("common.flag.wait_op"))

	return cmd
}

// finishVPCOp печатает операцию VPC или, с wait, ждёт её завершения.
// Команды «дождаться операцию VPC» в CLI нет — подсказки нет.
func finishVPCOp(cmd *cobra.Command, s *state.State, raw []byte, wait bool) error {
	op, ok := cloud.ParseVPCOperation(raw)

	return base.FinishOp(cmd, s, op, ok, wait, func(s *state.State) (operation.Getter, error) {
		v, err := s.VPC()
		if err != nil {
			return nil, err
		}

		return v.Operations(), nil
	}, "")
}

// withVPC — вызов VPC API с клиентом из state.
func withVPC(s *state.State, call func(*cloud.VPC) ([]byte, error)) ([]byte, error) {
	v, err := s.VPC()
	if err != nil {
		return nil, err
	}

	return call(v)
}
