// Package base — поведение, общее у команд всех ресурсов:
// подтверждение удаления, --wait, коды выхода, вывод сырого ответа API.
// Команда ресурса описывается полями DeleteCmd, ListCmd, DescribeCmd.
package base

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// ExitError — ошибка со своим кодом выхода.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }
func (e *ExitError) ExitCode() int { return e.Code }

// Call — вызов API команды; ответ — сырой JSON.
type Call func(ctx context.Context, s *state.State, args []string) ([]byte, error)

// Ops — опрос операций продукта для --wait.
type Ops func(s *state.State) (operation.Getter, error)

// ListCmd — команда чтения: List отдаёт ответ, --json печатает его как есть,
// Table — для человека; без Table и человеку печатается JSON.
type ListCmd struct {
	Use, Short string
	Aliases    []string
	Args       cobra.PositionalArgs
	List       Call
	Table      func(w io.Writer, raw []byte) error
}

// DescribeCmd — как ListCmd, для одного ресурса.
type DescribeCmd ListCmd

// DeleteCmd — удаление: без --yes отказ, ответ — операция.
type DeleteCmd struct {
	Use, Short string
	Args       cobra.PositionalArgs
	Delete     Call
	Ops        Ops
	// OpsCmd — команда операций продукта («redis operations get») для подсказки
	// после обрыва --wait; без NoWait у команды есть --wait.
	OpsCmd string
	NoWait bool
}

// Cmd собирает cobra-команду.
func (c ListCmd) Cmd(s *state.State) *cobra.Command {
	return &cobra.Command{
		Use: c.Use, Short: c.Short, Aliases: c.Aliases, Args: c.Args,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := c.List(cmd.Context(), s, args)
			if err != nil {
				return err
			}

			if c.Table == nil {
				return s.Printer.Object(raw, nil) // и для человека — JSON как есть
			}

			return s.Printer.Object(raw, func(w io.Writer) error { return c.Table(w, raw) })
		},
	}
}

// Cmd собирает cobra-команду.
func (c DescribeCmd) Cmd(s *state.State) *cobra.Command { return ListCmd(c).Cmd(s) }

// Cmd собирает cobra-команду.
func (c DeleteCmd) Cmd(s *state.State) *cobra.Command {
	var yes, wait bool

	cmd := &cobra.Command{
		Use: c.Use, Short: c.Short, Args: c.Args,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return &ExitError{Code: 1, Msg: i18n.T("common.err.need_yes")}
			}

			raw, err := c.Delete(cmd.Context(), s, args)
			if err != nil {
				return err
			}

			return Finish(cmd, s, raw, wait, c.Ops, c.OpsCmd)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, i18n.T("common.flag.yes"))

	if !c.NoWait {
		cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))
	}

	return cmd
}
