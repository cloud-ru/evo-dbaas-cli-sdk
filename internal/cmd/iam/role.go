package iam

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func rolesCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "roles", Short: i18n.T("iam.role.short")}
	c.AddCommand(roleListCmd(s), base.DescribeCmd{
		Use: "get PERMISSION_ID", Short: i18n.T("iam.role.get.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return with(s, func(c *cloud.IAM) ([]byte, error) { return c.Permission(ctx, args[0]) })
		},
	}.Cmd(s), grantCmd(s), confirmCmd(s, confirm{
		Use: "revoke PERMISSION_ID", Short: i18n.T("iam.role.revoke.short"), Args: cobra.ExactArgs(1),
		Refuse: i18n.T("iam.role.err.need_yes"), Flag: i18n.T("iam.role.revoke.flag.yes"), Done: func(id string) string { return i18n.T("iam.role.revoked", id) },
		Call: func(ctx context.Context, c *cloud.IAM, args []string) ([]byte, error) {
			return c.Revoke(ctx, args[0])
		},
	}))

	return c
}

func roleListCmd(s *state.State) *cobra.Command {
	var subject string

	cmd := base.ListCmd{
		Use: "list", Short: i18n.T("iam.role.list.short"), Args: cobra.NoArgs,
		List: func(ctx context.Context, s *state.State, _ []string) ([]byte, error) {
			project, err := s.NeedProject()
			if err != nil {
				return nil, err
			}

			filter := "resource_id=" + project
			if subject != "" {
				filter += " AND subject_id=" + subject
			}

			return with(s, func(c *cloud.IAM) ([]byte, error) { return c.Permissions(ctx, filter) })
		},
		Table: func(w io.Writer, raw []byte) error {
			t := output.NewTable(w, "ID", "ROLE", "SUBJECT", "SUBJECT_TYPE", "ENABLED")
			for _, p := range items(raw, "permissions") {
				m, _ := p.(map[string]any)
				t.Row(pick(m, "id"), pick(m, "role_name", "roleName", "role_id"), pick(m, "subject_id", "subjectId"),
					pick(m, "subject_type", "subjectType"), strconv.FormatBool(m["enabled"] == true))
			}

			return t.Flush()
		},
	}.Cmd(s)
	cmd.Flags().StringVar(&subject, "subject", "", i18n.T("iam.role.list.flag.subject"))

	return cmd
}

func grantCmd(s *state.State) *cobra.Command {
	var subject, role, expires string

	cmd := &cobra.Command{
		Use:   "grant",
		Short: i18n.T("iam.role.grant.short"),
		Long:  i18n.T("iam.role.grant.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, err := s.NeedProject()
			if err != nil {
				return err
			}

			raw, err := with(s, func(c *cloud.IAM) ([]byte, error) { return c.Grant(cmd.Context(), project, subject, role, expires) })
			if err != nil {
				return err
			}

			return s.Printer.Object(raw, func(w io.Writer) error {
				fmt.Fprintf(w, "%s\n", i18n.T("iam.role.granted", role, pick(object(raw), "id")))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&subject, "subject", "", i18n.T("iam.role.grant.flag.subject"))
	cmd.Flags().StringVar(&role, "role", "", i18n.T("iam.role.grant.flag.role"))
	cmd.Flags().StringVar(&expires, "expires", "", i18n.T("iam.role.grant.flag.expires"))
	_ = cmd.MarkFlagRequired("subject")
	_ = cmd.MarkFlagRequired("role")

	return cmd
}
