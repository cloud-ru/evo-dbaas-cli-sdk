// Package operation — redis operations: статус операции и операции кластера.
package operation

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// GetCmd — команда, которой дождаться операции Redis после обрыва --wait.
const GetCmd = "redis operations get"

// Ops — опрос операций Redis для --wait.
func Ops(s *state.State) (operation.Getter, error) {
	r, err := s.Redis()
	if err != nil {
		return nil, err
	}

	return r.Operations(), nil
}

// With — вызов клиента Redis: ошибка создания клиента возвращается как есть.
func With(s *state.State, call func(*cloud.Redis) ([]byte, error)) ([]byte, error) {
	c, err := s.Redis()
	if err != nil {
		return nil, err
	}

	return call(c)
}

// NewCmd — redis operations.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "operations", Aliases: []string{"op", "operation"}, Short: i18n.T("redis.operation.short")}
	c.AddCommand(getCmd(s), listCmd(s))

	return c
}

func getCmd(s *state.State) *cobra.Command {
	var wait bool

	g := &cobra.Command{
		Use: "get OPERATION_ID", Short: i18n.T("redis.operation.get.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Первый запрос — без ожидания: опечатка в id даёт 404 и код 1, как
			// без --wait, а не код 8 «операция могла продолжиться».
			raw, err := With(s, func(r *cloud.Redis) ([]byte, error) { return r.Operation(cmd.Context(), args[0]) })
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

func listCmd(s *state.State) *cobra.Command {
	return base.ListCmd{
		Use: "list CLUSTER_ID", Short: i18n.T("redis.operation.list.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return With(s, func(r *cloud.Redis) ([]byte, error) { return r.ClusterOperations(ctx, args[0]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			var l listed
			if err := output.Decode(raw, &l); err != nil {
				return err
			}

			t := output.NewTable(w, "ID", "DONE", "CREATED", "DESCRIPTION")
			for _, op := range l.Operations {
				created := ""
				if op.CreatedAt != nil {
					created = op.CreatedAt.Format(time.RFC3339)
				}

				t.Row(op.ID, fmt.Sprint(op.Done), created, op.Description)
			}

			return t.Flush()
		},
	}.Cmd(s)
}
