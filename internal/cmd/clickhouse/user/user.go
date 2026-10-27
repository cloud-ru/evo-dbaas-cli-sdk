// Package user — clickhouse users: файл на глагол.
package user

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — clickhouse users.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "users", Aliases: []string{"user"}, Short: i18n.T("clickhouse.user.short")}
	c.AddCommand(listCmd(s), describeCmd(s), createCmd(s), updateCmd(s), deleteCmd(s),
		roleCmd(s, "grant", i18n.T("clickhouse.user.grant.short")), roleCmd(s, "revoke", i18n.T("clickhouse.user.revoke.short")))

	return c
}

// user — пользователь в ответе API: роли — в granted_roles, у старых — в roles.
type user struct {
	Name         string `json:"name"`
	GrantedRoles *struct {
		Value *[]string `json:"value"`
	} `json:"granted_roles"`
	Roles    *[]string `json:"roles"`
	ReadOnly bool      `json:"read_only"`
}

func (u user) roles() string {
	switch {
	case u.GrantedRoles != nil && u.GrantedRoles.Value != nil:
		return strings.Join(*u.GrantedRoles.Value, ",")
	case u.Roles != nil:
		return strings.Join(*u.Roles, ",")
	}

	return ""
}

func listCmd(s *state.State) *cobra.Command {
	return base.ListCmd{
		Use: "list CLUSTER_ID", Short: i18n.T("clickhouse.user.list.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Users(ctx, args[0]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l struct {
				Users []user `json:"users"`
			}
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "NAME", "ROLES", "READ_ONLY")
			for _, u := range l.Users {
				t.Row(u.Name, u.roles(), fmt.Sprint(u.ReadOnly))
			}

			return t.Flush()
		},
	}.Cmd(s)
}

func describeCmd(s *state.State) *cobra.Command {
	return base.DescribeCmd{
		Use: "get CLUSTER_ID USER_NAME", Short: i18n.T("clickhouse.user.get.short"), Args: cobra.ExactArgs(2),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.User(ctx, args[0], args[1]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var u user
			if err := output.Decode(raw, &u); err != nil {
				return err
			}

			fmt.Fprintf(w, "%s\t%s\n", u.Name, u.roles())

			return nil
		},
	}.Cmd(s)
}

func deleteCmd(s *state.State) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use: "delete CLUSTER_ID USER_NAME", Short: i18n.T("clickhouse.user.delete.short"), Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return &base.ExitError{Code: 1, Msg: i18n.T("common.err.need_yes")}
			}

			raw, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.DeleteUser(cmd.Context(), args[0], args[1]) })

			return done(s, raw, err, "%s", i18n.T("clickhouse.user.deleted", args[1]))
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, i18n.T("common.flag.yes"))

	return cmd
}

func roleCmd(s *state.State, verb, short string) *cobra.Command {
	return &cobra.Command{
		Use: verb + " CLUSTER_ID USER_NAME ROLE", Short: short, Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) {
				if verb == "grant" {
					return h.GrantRole(cmd.Context(), args[0], args[1], args[2])
				}

				return h.RevokeRole(cmd.Context(), args[0], args[1], args[2])
			})

			return done(s, raw, err, "%s: %s → %s", verb, args[2], args[1])
		},
	}
}

// done печатает ответ API как есть, человеку — сообщение: у пользователей
// ClickHouse ответ — не операция.
func done(s *state.State, raw []byte, err error, format string, a ...any) error {
	if err != nil {
		return err
	}

	return s.Printer.Object(raw, func(io.Writer) error { s.Printer.Messagef(format, a...); return nil })
}
