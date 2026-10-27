package iam

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func serviceAccountsCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "service-accounts", Aliases: []string{"sa"}, Short: i18n.T("iam.serviceaccount.short")}
	c.AddCommand(base.ListCmd{
		Use: "list", Short: i18n.T("iam.serviceaccount.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			project, err := s.NeedProject()
			if err != nil {
				return nil, err
			}

			return with(s, func(c *cloud.IAM) ([]byte, error) { return c.ServiceAccounts(ctx, project) })
		},
		Table: func(w io.Writer, raw []byte) error {
			t := output.NewTable(w, "ID", "NAME", "TYPE", "ENABLED")
			for _, it := range items(raw, "service_accounts", "serviceAccounts") {
				m, _ := it.(map[string]any)
				t.Row(pick(m, "id"), pick(m, "name"), pick(m, "type"), fmt.Sprint(m["enabled"]))
			}

			return t.Flush()
		},
	}.Cmd(s), base.DescribeCmd{
		Use: "get SERVICE_ACCOUNT_ID", Short: i18n.T("iam.serviceaccount.get.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return with(s, func(c *cloud.IAM) ([]byte, error) { return c.ServiceAccount(ctx, args[0]) })
		},
	}.Cmd(s), serviceAccountCreateCmd(s), confirmCmd(s, confirm{
		Use: "delete SERVICE_ACCOUNT_ID", Short: i18n.T("iam.serviceaccount.delete.short"), Args: cobra.ExactArgs(1),
		Refuse: i18n.T("common.err.need_yes"), Flag: i18n.T("common.flag.yes"), Done: func(id string) string { return i18n.T("iam.serviceaccount.deleted", id) },
		Call: func(ctx context.Context, c *cloud.IAM, args []string) ([]byte, error) {
			return c.DeleteServiceAccount(ctx, args[0])
		},
	}))

	return c
}

func serviceAccountCreateCmd(s *state.State) *cobra.Command {
	var name, desc string

	cmd := &cobra.Command{
		Use: "create", Short: i18n.T("iam.serviceaccount.create.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, err := s.NeedProject()
			if err != nil {
				return err
			}

			raw, err := with(s, func(c *cloud.IAM) ([]byte, error) { return c.CreateServiceAccount(cmd.Context(), project, name, desc) })
			if err != nil {
				return err
			}

			return s.Printer.Object(raw, func(w io.Writer) error {
				m := object(raw)
				fmt.Fprintf(w, "%s\n", i18n.T("iam.serviceaccount.created", pick(m, "id"), pick(m, "name")))

				return nil
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", i18n.T("iam.serviceaccount.create.flag.name"))
	cmd.Flags().StringVar(&desc, "description", "", i18n.T("common.flag.description"))
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

// confirm — необратимая команда IAM: без --yes отказ, ответ API печатается
// как есть, человеку — сообщение Done с последним аргументом.
type confirm struct {
	Use, Short, Refuse, Flag string
	Done                     func(string) string
	Args                     cobra.PositionalArgs
	Call                     func(ctx context.Context, c *cloud.IAM, args []string) ([]byte, error)
}

func confirmCmd(s *state.State, c confirm) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use: c.Use, Short: c.Short, Args: c.Args,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return &base.ExitError{Code: 1, Msg: c.Refuse}
			}

			raw, err := with(s, func(i *cloud.IAM) ([]byte, error) { return c.Call(cmd.Context(), i, args) })
			if err != nil {
				return err
			}

			return s.Printer.Object(raw, func(io.Writer) error { s.Printer.Messagef("%s", c.Done(args[len(args)-1])); return nil })
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, c.Flag)

	return cmd
}
