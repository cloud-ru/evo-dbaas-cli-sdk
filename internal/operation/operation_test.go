package operation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// script — Getter, отдающий ответы по порядку; последний повторяется.
func script(t *testing.T, replies ...func() (Operation, error)) (Getter, *int) {
	t.Helper()

	calls := 0

	return func(_ context.Context, id string) (Operation, error) {
		assert.Equal(t, "op1", id)

		i := min(calls, len(replies)-1)
		calls++

		return replies[i]()
	}, &calls
}

func reply(op Operation) func() (Operation, error) {
	return func() (Operation, error) { op.ID = "op1"; return op, nil }
}

func fail(err error) func() (Operation, error) {
	return func() (Operation, error) { return Operation{}, err }
}

var fast = Options{Timeout: time.Second, Delay: time.Millisecond}

func TestWait(t *testing.T) {
	t.Parallel()

	errNet := errors.New("unexpected EOF")

	tests := []struct {
		name      string
		replies   []func() (Operation, error)
		wantCalls int
		wantErr   func(t *testing.T, err error)
	}{
		{"done after polls", []func() (Operation, error){reply(Operation{}), reply(Operation{Done: true, ResourceID: "c1"})}, 2, nil},
		{"failed without message", []func() (Operation, error){reply(Operation{Done: true, Failed: true})}, 1, func(t *testing.T, err error) {
			t.Helper()

			var fe *FailedError
			require.ErrorAs(t, err, &fe)
			assert.Equal(t, "op1", fe.Op.ID)
		}},
		{"transient failures are retried", []func() (Operation, error){fail(Transient(errNet)), reply(Operation{Done: true})}, 2, nil},
		{"transient failures in a row give up", []func() (Operation, error){fail(Transient(errNet))}, 5, func(t *testing.T, err error) {
			t.Helper()

			var pe *PendingError
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, "op1", pe.ID)
			assert.ErrorIs(t, err, errNet)
		}},
		{"other poll error is pending at once", []func() (Operation, error){fail(errNet)}, 1, func(t *testing.T, err error) {
			t.Helper()
			require.ErrorIs(t, err, ErrPending)
			assert.ErrorIs(t, err, errNet)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			get, calls := script(t, tt.replies...)
			_, err := NewWaiter(get, fast).Wait(t.Context(), Operation{ID: "op1"})

			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				tt.wantErr(t, err)
			}

			assert.Equal(t, tt.wantCalls, *calls)
		})
	}
}

func TestWaitDoneOperationIsNotPolled(t *testing.T) {
	t.Parallel()

	get, calls := script(t, reply(Operation{}))

	_, err := NewWaiter(get, fast).Wait(t.Context(), Operation{ID: "op1", Done: true, Failed: true, Message: "busy"})

	var fe *FailedError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, "busy", fe.Op.Message)
	assert.Zero(t, *calls)
}

func TestWaitTimeout(t *testing.T) {
	t.Parallel()

	get, _ := script(t, reply(Operation{}))
	_, err := NewWaiter(get, Options{Timeout: 20 * time.Millisecond, Delay: time.Millisecond}).Wait(t.Context(), Operation{ID: "op1"})

	var pe *PendingError
	require.ErrorAs(t, err, &pe)
	require.NoError(t, pe.Cause, "истёк срок — причины нет, текст говорит о --timeout")
	assert.Equal(t, 20*time.Millisecond, pe.Timeout)
}

func TestWaitDoneAfterDeadlineIsDone(t *testing.T) {
	t.Parallel()

	get := func(context.Context, string) (Operation, error) {
		time.Sleep(40 * time.Millisecond) // ответ «готово» пришёл после --timeout
		return Operation{ID: "op1", Done: true}, nil
	}

	op, err := NewWaiter(get, Options{Timeout: 20 * time.Millisecond, Delay: time.Millisecond}).Wait(t.Context(), Operation{ID: "op1"})

	require.NoError(t, err)
	assert.True(t, op.Done)
}

func TestWaitCanceledByParent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	get := func(context.Context, string) (Operation, error) {
		cancel() // Ctrl-C во время опроса
		return Operation{ID: "op1"}, nil
	}

	_, err := NewWaiter(get, fast).Wait(ctx, Operation{ID: "op1"})

	require.ErrorIs(t, err, ErrPending)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWaitReportsNewDescriptionOnce(t *testing.T) {
	t.Parallel()

	var seen []string

	get, _ := script(t, reply(Operation{Description: "step 1"}), reply(Operation{Description: "step 1"}),
		reply(Operation{Description: "step 2", Done: true}))
	opts := fast
	opts.Progress = func(desc string) { seen = append(seen, desc) }

	_, err := NewWaiter(get, opts).Wait(t.Context(), Operation{ID: "op1"})

	require.NoError(t, err)
	assert.Equal(t, []string{"step 1", "step 2"}, seen)
}

func TestUntilNamesWhatIsAwaited(t *testing.T) {
	t.Parallel()

	err := Until(t.Context(), "subnet s1", Options{Timeout: 10 * time.Millisecond, Delay: time.Millisecond},
		func(context.Context) (bool, error) { return false, nil })

	var pe *PendingError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, "subnet s1", pe.What)
	assert.Empty(t, pe.ID)
}
