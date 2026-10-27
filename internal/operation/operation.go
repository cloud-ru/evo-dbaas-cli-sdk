// Package operation — ожидание долгих операций облака: опрос до done, пауза
// между опросами, терпимость к временным сбоям и предел --timeout. Одно место
// для DBaaS (ClickHouse, Redis, Kafka), VPC и подсетей; тексты пользователю — в output.
package operation

import (
	"context"
	"errors"
	"time"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Operation — операция в общем виде: схема DBaaS и VPC сводятся к ней.
type Operation struct {
	ID          string
	ResourceID  string
	Description string
	Done        bool
	// Failed — в ответе есть error, даже без message: error без message — тоже
	// ошибка, не успех.
	Failed  bool
	Message string
	// Raw — тело ответа, из которого собрана операция: --json печатает его как есть.
	Raw []byte
}

// Getter запрашивает операцию по id. Ошибку, после которой опрос стоит
// повторить, оборачивает Transient.
type Getter func(ctx context.Context, id string) (Operation, error)

// ErrPending — ожидание кончилось, а операция в облаке, возможно, идёт:
// повтор create дал бы второй ресурс, поэтому это не обычная ошибка.
var ErrPending = i18n.Sentinel("operation.err.pending")

// PendingError — ожидание оборвалось: истёк Timeout, прервали снаружи
// (Cause — context.Canceled) или опрос не удался (Cause — его ошибка).
type PendingError struct {
	// ID — операция, которую ждали; What — что ждали, если это не операция (подсеть).
	ID, What string
	Timeout  time.Duration
	Cause    error
}

func (e *PendingError) Error() string        { return e.ID + e.What + ": " + ErrPending.Error() }
func (e *PendingError) Is(target error) bool { return target == ErrPending }
func (e *PendingError) Unwrap() error        { return e.Cause }

// FailedError — операция завершилась ошибкой.
type FailedError struct{ Op Operation }

func (e *FailedError) Error() string { return "operation " + e.Op.ID + " failed: " + e.Op.Message }

type transientError struct{ err error }

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

// Transient помечает сбой опроса как временный (сеть, 429, 5xx).
func Transient(err error) error { return &transientError{err} }

// MaxFailures — сколько временных сбоев опроса подряд терпит ожидание:
// 9.10 Kafka уронила 60-минутное ожидание одним `unexpected EOF`, хотя
// операция в облаке шла дальше.
const MaxFailures = 5

// Options — настройки ожидания; нулевая Delay — 5 с, nil-функции молчат.
type Options struct {
	Timeout time.Duration
	// Delay — первая пауза между опросами; дальше растёт до 4×Delay.
	Delay time.Duration
	// Progress получает новое описание операции, Retry — временный сбой опроса.
	Progress func(desc string)
	Retry    func(err error)
}

// Waiter ждёт операции одного API.
type Waiter struct {
	get  Getter
	opts Options
}

func NewWaiter(get Getter, opts Options) *Waiter { return &Waiter{get: get, opts: opts} }

// Wait опрашивает op до done. Завершённая с ошибкой — *FailedError вместе с
// самой операцией; оборванное ожидание — *PendingError.
func (w *Waiter) Wait(ctx context.Context, op Operation) (Operation, error) {
	if op.Done {
		if op.Failed {
			return op, &FailedError{op}
		}

		return op, nil
	}

	lastDesc := ""
	id := op.ID
	err := Until(ctx, "", w.opts, func(ctx context.Context) (bool, error) {
		got, err := w.get(ctx, id)
		if err != nil {
			return false, err
		}

		op = got
		if op.Description != "" && op.Description != lastDesc && w.opts.Progress != nil {
			w.opts.Progress(op.Description)
			lastDesc = op.Description
		}

		if op.Done && op.Failed {
			return false, &FailedError{op}
		}

		return op.Done, nil
	})

	var pe *PendingError
	if errors.As(err, &pe) {
		pe.ID = id
	}

	return op, err
}

// Until опрашивает done до true; what — что ждём, если это не операция
// (подсеть). Временный сбой (Transient) повторяется до MaxFailures раз
// подряд; другой сбой и конец времени — *PendingError: в облаке операция
// могла продолжиться. *FailedError проходит как есть.
func Until(parent context.Context, what string, opts Options, done func(context.Context) (bool, error)) error {
	if opts.Delay == 0 {
		opts.Delay = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(parent, opts.Timeout)
	defer cancel()

	pending := func(cause error) error {
		switch {
		case parent.Err() != nil:
			cause = parent.Err()
		case ctx.Err() != nil:
			cause = nil
		}

		return &PendingError{What: what, Timeout: opts.Timeout, Cause: cause}
	}

	delay, failures := opts.Delay, 0

	for {
		ok, err := done(ctx)

		var (
			failed *FailedError
			tr     *transientError
		)

		switch {
		case errors.As(err, &failed):
			return err
		case err == nil && ok: // готово, даже если срок истёк, пока шёл запрос
			return nil
		case ctx.Err() != nil:
			return pending(nil)
		case errors.As(err, &tr):
			failures++
			if failures >= MaxFailures {
				return pending(err)
			}

			if opts.Retry != nil {
				opts.Retry(err)
			}
		case err != nil:
			return pending(err)
		default:
			failures = 0
		}

		select {
		case <-ctx.Done():
			return pending(nil)
		case <-time.After(delay):
		}

		if failures == 0 && delay < 4*opts.Delay {
			delay += opts.Delay
		}
	}
}
