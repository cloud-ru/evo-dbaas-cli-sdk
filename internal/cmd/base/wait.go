package base

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// Finish печатает операцию из ответа raw сразу или, с wait, после её
// завершения. opsCmd — команда операций продукта для подсказки после обрыва.
func Finish(cmd *cobra.Command, s *state.State, raw []byte, wait bool, ops Ops, opsCmd string) error {
	op, ok := cloud.ParseOperation(raw)

	return FinishOp(cmd, s, op, ok, wait, ops, cmd.Root().Name()+" "+opsCmd)
}

// FinishOp — Finish над уже разобранной операцией (ok — в ответе операция).
// hint — команда, которой дождаться операции после обрыва; "" — такой нет.
func FinishOp(cmd *cobra.Command, s *state.State, op operation.Operation, ok, wait bool, ops Ops, hint string) error {
	if !wait {
		return printOp(s, op, ok)
	}

	if !ok {
		// Без операции --wait не выходит молча с кодом 0: ждать нечего — это ошибка.
		_ = printOp(s, op, false)
		return &ExitError{Code: 8, Msg: i18n.T("base.err.wait_no_operation")}
	}

	done, err := waitOp(cmd, s, op, ops, hint)
	if err != nil {
		return err
	}

	return printOp(s, done, true)
}

// AwaitCreated — create: без wait печатает операцию, с wait ждёт её и отдаёт
// id созданного ресурса. "" без ошибки — печатать больше нечего: у
// завершённой операции нет resource_id (напечатана она и подсказка про list).
func AwaitCreated(cmd *cobra.Command, s *state.State, raw []byte, wait bool, ops Ops, opsCmd, list string) (string, error) {
	op, ok := cloud.ParseOperation(raw)
	if !wait || !ok {
		return "", Finish(cmd, s, raw, wait, ops, opsCmd)
	}

	done, err := waitOp(cmd, s, op, ops, cmd.Root().Name()+" "+opsCmd)
	if err != nil {
		return "", err
	}

	if done.ResourceID == "" {
		s.Printer.Messagef("%s", i18n.T("base.wait.no_resource_id", list))
		return "", printOp(s, done, true)
	}

	return done.ResourceID, nil
}

// waitOp ждёт операцию; при неудаче печатает в stdout, чем кончилось: в --json
// агенту нужны id операции и причина, а не только текст stderr.
func waitOp(cmd *cobra.Command, s *state.State, op operation.Operation, ops Ops, hint string) (operation.Operation, error) {
	get, err := ops(s)
	if err != nil {
		return op, err
	}

	done, err := operation.NewWaiter(get, WaitOptions(s)).Wait(cmd.Context(), op)
	if err == nil {
		return done, nil
	}

	var fe *operation.FailedError

	switch {
	case errors.As(err, &fe):
		_ = printOp(s, fe.Op, true)
	case s.Printer.JSON && len(op.Raw) > 0:
		_ = s.Printer.Object(op.Raw, nil)
	}

	return done, output.OperationError(err, hint)
}

// WaitOptions — предел и шаг опроса из state, прогресс — в stderr.
func WaitOptions(s *state.State) operation.Options {
	return operation.Options{
		Timeout: s.Timeout, Delay: s.PollDelay,
		Progress: func(desc string) { s.Printer.Progressf("%s", i18n.T("base.wait.progress", desc)) },
		Retry:    func(err error) { s.Printer.Progressf("%s", i18n.T("base.wait.retry", err)) },
	}
}

// printOp печатает операцию; ok=false — в ответе не операция, человеку печатать нечего.
func printOp(s *state.State, op operation.Operation, ok bool) error {
	return s.Printer.Object(op.Raw, func(w io.Writer) error {
		if ok {
			PrintOperation(w, op)
		}

		return nil
	})
}

// PrintOperation — операция для человека.
func PrintOperation(w io.Writer, op operation.Operation) {
	state := i18n.T("base.operation.pending")
	if op.Done {
		state = i18n.T("base.operation.done")
		if op.Failed {
			state = i18n.T("base.operation.failed")
			if op.Message != "" {
				state = strings.TrimSuffix(i18n.T("base.operation.failed_message", op.Message), ": ")
			}
		}
	}

	fmt.Fprintf(w, "%s\n", i18n.T("base.operation.status", op.ID, state))

	if op.ResourceID != "" {
		fmt.Fprintf(w, "%s\n", i18n.T("base.operation.resource", op.ResourceID))
	}

	if op.Description != "" {
		fmt.Fprintf(w, "          %s\n", op.Description)
	}
}
