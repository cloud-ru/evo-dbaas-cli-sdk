// Package database — clickhouse databases.
package database

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

type database struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
}

// NewCmd — clickhouse databases.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "databases", Aliases: []string{"db"}, Short: i18n.T("clickhouse.database.short")}
	c.AddCommand(
		base.ListCmd{
			Use: "list CLUSTER_ID", Short: i18n.T("clickhouse.database.list.short"), Args: cobra.ExactArgs(1),
			List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
				return operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Databases(ctx, args[0]) })
			},
			Table: func(w io.Writer, raw []byte) error {
				var l struct {
					Databases []database `json:"databases"`
				}
				if err := output.Decode(raw, &l); err != nil {
					return err
				}

				t := output.NewTable(w, "NAME", "ENGINE")
				for _, d := range l.Databases {
					t.Row(d.Name, d.Engine)
				}

				return t.Flush()
			},
		}.Cmd(s),
		&cobra.Command{
			Use: "create CLUSTER_ID NAME", Short: i18n.T("clickhouse.database.create.short"), Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				raw, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.CreateDatabase(cmd.Context(), args[0], args[1]) })
				if err != nil {
					return err
				}

				return s.Printer.Object(raw, func(io.Writer) error {
					s.Printer.Messagef("%s", i18n.T("clickhouse.database.created", args[1]))
					return nil
				})
			},
		},
		base.DescribeCmd{
			Use: "get CLUSTER_ID NAME", Short: i18n.T("clickhouse.database.get.short"), Args: cobra.ExactArgs(2),
			List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
				return operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Database(ctx, args[0], args[1]) })
			},
			Table: func(w io.Writer, raw []byte) error {
				var d database
				if err := output.Decode(raw, &d); err != nil {
					return err
				}

				fmt.Fprintf(w, "%s\t%s\n", d.Name, d.Engine)

				return nil
			},
		}.Cmd(s),
		deleteCmd(s),
	)

	return c
}

func deleteCmd(s *state.State) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use: "delete CLUSTER_ID NAME", Short: i18n.T("clickhouse.database.delete.short"), Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return &base.ExitError{Code: 1, Msg: i18n.T("common.err.need_yes")}
			}

			raw, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.DeleteDatabase(cmd.Context(), args[0], args[1]) })
			if err != nil {
				return err
			}

			return s.Printer.Object(raw, func(io.Writer) error {
				s.Printer.Messagef("%s", i18n.T("clickhouse.database.deleted", args[1]))
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, i18n.T("common.flag.yes"))

	return cmd
}
