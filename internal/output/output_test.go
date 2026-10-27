package output

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/auth"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

func TestPrinterObject(t *testing.T) {
	t.Parallel()

	human := func(w io.Writer) error {
		fmt.Fprint(w, "human\n")
		return nil
	}

	tests := []struct {
		name  string
		json  bool
		v     any
		human func(w io.Writer) error
		want  string
	}{
		{"human", false, []byte(`{"a":1}`), human, "human\n"},
		{"json flag wins over human", true, []byte(`{"a":1}`), human, "{\"a\":1}\n"},
		{"no human prints json", false, []byte(`{"a":1}`), nil, "{\"a\":1}\n"},
		{"body is compacted", true, []byte("{\n  \"a\": 1\n}"), nil, "{\"a\":1}\n"},
		{"broken body is printed as is", true, []byte("not json"), nil, "not json\n"},
		{"body keeps unknown fields and big numbers", true, []byte(`{"new_field":{"x":12345678901234567890}}`), nil, "{\"new_field\":{\"x\":12345678901234567890}}\n"},
		{"value is encoded", true, map[string]int{"a": 1}, nil, "{\"a\":1}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer

			p := &Printer{Out: &out, Err: &errOut, JSON: tt.json}
			require.NoError(t, p.Object(tt.v, tt.human))
			assert.Equal(t, tt.want, out.String())
			assert.Empty(t, errOut.String())
		})
	}
}

func TestPrinterObjectMarshalError(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	p := &Printer{Out: &out, Err: io.Discard, JSON: true}
	require.Error(t, p.Object(make(chan int), nil))
	assert.Empty(t, out.String())
}

func TestPrinterMessages(t *testing.T) {
	t.Parallel()

	for _, jsonMode := range []bool{false, true} {
		var out, errOut bytes.Buffer

		p := &Printer{Out: &out, Err: &errOut, JSON: jsonMode}
		p.Messagef("deleted %s", "x")
		p.Progressf("… %s", "step")

		want := "deleted x\n"
		if !jsonMode {
			want += "… step\n"
		}

		assert.Empty(t, out.String(), "json=%v", jsonMode)
		assert.Equal(t, want, errOut.String(), "json=%v", jsonMode)
	}
}

func TestTable(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer

	tb := NewTable(&b, "ID", "NAME")
	tb.Row("1", "long-name")
	require.NoError(t, tb.Flush())
	assert.Equal(t, "ID  NAME\n1   long-name\n", b.String())

	b.Reset()

	tb = NewTable(&b)
	tb.Field("id", "1")
	tb.Field("status", "RUNNING")
	require.NoError(t, tb.Flush())
	assert.Equal(t, "id:      1\nstatus:  RUNNING\n", b.String())
}

func TestAuthError(t *testing.T) {
	t.Parallel()

	other := errors.New("dial tcp: refused")

	tests := []struct {
		name string
		err  error
		want string
		is   error
	}{
		{"no key", auth.ErrNoKey, "access key not set: set CLOUDRU_KEY_ID and CLOUDRU_KEY_SECRET", auth.ErrNoKey},
		{"401", &auth.IAMError{Status: 401, Body: "bad"}, "IAM returned 401: bad — key rejected; check CLOUDRU_KEY_ID and CLOUDRU_KEY_SECRET", auth.ErrKeyRejected},
		{"403", &auth.IAMError{Status: 403, Body: "off"}, "IAM returned 403: off — key recognized, but no token issued: check whether the key or service account is disabled", auth.ErrKeyForbidden},
		{"429", &auth.IAMError{Status: 429, Body: "slow"}, "IAM returned 429: slow — too many requests; try again in a minute", auth.ErrRateLimited},
		{"other status", &auth.IAMError{Status: 500, Body: "boom"}, "IAM returned 500: boom", nil},
		{"not auth", other, "dial tcp: refused", other},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := AuthError(tt.err)
			require.Error(t, got)
			assert.Equal(t, tt.want, got.Error())

			if tt.is != nil {
				require.ErrorIs(t, got, tt.is)
			}

			var ie *auth.IAMError
			if errors.As(tt.err, &ie) {
				assert.ErrorAs(t, got, &ie)
			}
		})
	}

	assert.NoError(t, AuthError(nil))
}

func TestOperationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		hint string
		want string
	}{
		{
			"failed", &operation.FailedError{Op: operation.Operation{ID: "op1", Message: "busy"}}, "",
			"operation op1 failed: busy",
		},
		{
			"failed without message", &operation.FailedError{Op: operation.Operation{ID: "op1", Failed: true}}, "",
			"operation op1 failed: no description",
		},
		{
			"timeout with hint", &operation.PendingError{ID: "op1", Timeout: time.Minute}, "x redis operations get",
			"operation op1 did not finish within 1m0s; it may still be running in the cloud; wait with: `x redis operations get op1 --wait`",
		},
		{
			"interrupted", &operation.PendingError{ID: "op1", Cause: context.Canceled}, "x operations get",
			"operation op1 — waiting interrupted; it may still be running in the cloud; wait with: `x operations get op1 --wait`",
		},
		{
			"poll failed, not an operation", &operation.PendingError{What: "deleting subnet s1", Cause: errors.New("API 500")}, "",
			"deleting subnet s1 — polling failed: API 500; it may still be running in the cloud; check its state before retrying",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := OperationError(tt.err, tt.hint)
			require.Error(t, got)
			assert.Equal(t, tt.want, got.Error())
			assert.ErrorIs(t, got, tt.err, "исходная ошибка видна через errors.Is")
		})
	}

	plain := errors.New("x")
	assert.Equal(t, plain, OperationError(plain, ""))
}

type codedError struct{ code int }

func (e codedError) Error() string { return "coded" }
func (e codedError) ExitCode() int { return e.code }

func TestExitCode(t *testing.T) {
	t.Parallel()

	pending := &operation.PendingError{ID: "op1"}

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain", errors.New("x"), 1},
		{"canceled", fmt.Errorf("get: %w", context.Canceled), 2},
		{"interrupted wait is cancel, not pending", &operation.PendingError{ID: "op1", Cause: context.Canceled}, 2},
		{"pending", OperationError(pending, ""), 8},
		{"failed operation", &operation.FailedError{}, 1},
		{"no key", AuthError(auth.ErrNoKey), 4},
		{"key rejected", fmt.Errorf("iam: %w", auth.ErrKeyRejected), 4},
		{"key forbidden", auth.ErrKeyForbidden, 4},
		{"rate limited is not auth", auth.ErrRateLimited, 1},
		{"own code", fmt.Errorf("wrap: %w", codedError{4}), 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ExitCode(tt.err))
		})
	}
}
