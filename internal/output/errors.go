package output

import (
	"cmp"
	"context"
	"errors"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/auth"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// userError — текст для человека поверх ошибки нижнего слоя; errors.Is/As видят исходную.
type userError struct {
	msg string
	err error
}

func (e *userError) Error() string { return e.msg }
func (e *userError) Unwrap() error { return e.err }

// AuthError переводит ошибки auth в тексты пользователю (текст — наверху,
// внизу — sentinel).
func AuthError(err error) error {
	var ie *auth.IAMError

	switch {
	case err == nil:
		return nil
	case errors.Is(err, auth.ErrNoKey):
		return &userError{i18n.T("output.auth.no_key"), err}
	case !errors.As(err, &ie):
		return err
	case errors.Is(err, auth.ErrKeyRejected):
		return &userError{i18n.T("output.auth.rejected", ie.Body), err}
	case errors.Is(err, auth.ErrKeyForbidden):
		return &userError{i18n.T("output.auth.forbidden", ie.Body), err}
	case errors.Is(err, auth.ErrRateLimited):
		return &userError{i18n.T("output.auth.rate_limited", ie.Body), err}
	}

	return &userError{i18n.T("output.auth.other", ie.Status, ie.Body), err}
}

// OperationError переводит ошибки ожидания в тексты пользователю; hint — команда,
// которой дождаться операции («cloudru-dbaas redis operations get»).
func OperationError(err error, hint string) error {
	var (
		pe *operation.PendingError
		fe *operation.FailedError
	)

	switch {
	case errors.As(err, &fe):
		return &userError{i18n.T("output.operation.failed", fe.Op.ID, cmp.Or(fe.Op.Message, i18n.T("output.operation.no_description"))), err}
	case !errors.As(err, &pe):
		return err
	}

	what := pe.What
	if what == "" {
		what = i18n.T("output.operation.name", pe.ID)
	}

	return &userError{pendingMessage(pe, what, hint), err}
}

func pendingMessage(pe *operation.PendingError, what, hint string) string {
	withHint := hint != "" && pe.ID != ""

	switch {
	case errors.Is(pe.Cause, context.Canceled):
		if withHint {
			return i18n.T("output.operation.canceled_hint", what, hint, pe.ID)
		}

		return i18n.T("output.operation.canceled", what)
	case pe.Cause != nil:
		if withHint {
			return i18n.T("output.operation.poll_hint", what, pe.Cause.Error(), hint, pe.ID)
		}

		return i18n.T("output.operation.poll", what, pe.Cause.Error())
	case pe.What == "":
		if withHint {
			return i18n.T("output.operation.timeout_operation_hint", what, pe.Timeout, hint, pe.ID)
		}

		return i18n.T("output.operation.timeout_operation", what, pe.Timeout)
	default:
		if withHint {
			return i18n.T("output.operation.timeout_hint", what, pe.Timeout, hint, pe.ID)
		}

		return i18n.T("output.operation.timeout", what, pe.Timeout)
	}
}
