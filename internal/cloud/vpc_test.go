package cloud

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

func TestParseVPCOperation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, raw, message string
		failed             bool
	}{
		{"success", ` {"id":"op1","resourceId":"v1","done":true,"extra":17} `, "", false},
		{"error", `{"id":"op1","resourceId":"v1","done":true,"error":{"message":"quota"}}`, "quota", true},
		{"empty error", `{"id":"op1","resourceId":"v1","done":true,"error":{}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op, ok := ParseVPCOperation([]byte(tc.raw))
			require.True(t, ok)
			assert.Equal(t, "op1", op.ID)
			assert.Equal(t, "v1", op.ResourceID)
			assert.True(t, op.Done)
			assert.Equal(t, tc.failed, op.Failed)
			assert.Equal(t, tc.message, op.Message)
			assert.Equal(t, []byte(tc.raw), op.Raw)
		})
	}

	for _, raw := range []string{"", `not JSON`, `null`, `{}`, `{"done":true}`, `{"id":""}`} {
		op, ok := ParseVPCOperation([]byte(raw))
		assert.False(t, ok)
		assert.Equal(t, []byte(raw), op.Raw)
	}
}

func TestVPCOperationsRetriesTemporaryFailures(t *testing.T) {
	t.Parallel()

	for _, status := range []int{0, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			networkErr := errors.New("connection lost")
			server := serve(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET /v1/vpcs/operations/op1", r.Method+" "+r.URL.Path)
				assert.Empty(t, r.URL.RawQuery)

				if calls.Load() == 1 {
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"message":"temporary failure"}`)

					return
				}

				_, _ = io.WriteString(w, `{"id":"op1","done":true,"resourceId":"v1"}`)
			})
			opts := noAuth
			opts.HTTPClient = &http.Client{Transport: redisRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 && status == 0 {
					return nil, fmt.Errorf("test transport: %w", networkErr)
				}

				response, err := testTransport.RoundTrip(r)
				if err != nil {
					return nil, fmt.Errorf("test transport: %w", err)
				}

				return response, nil
			})}
			v, err := NewVPC(server, opts)
			require.NoError(t, err)

			retries := 0
			op, err := operation.NewWaiter(v.Operations(), operation.Options{Timeout: time.Second, Delay: time.Millisecond, Retry: func(err error) {
				retries++

				if status == 0 {
					require.ErrorIs(t, err, networkErr)
				} else {
					var ae *APIError
					require.ErrorAs(t, err, &ae)
					assert.Equal(t, status, ae.Status)
				}
			}}).Wait(t.Context(), operation.Operation{ID: "op1"})
			require.NoError(t, err)
			assert.True(t, op.Done)
			assert.Equal(t, "v1", op.ResourceID)
			assert.Equal(t, 1, retries)
			assert.Equal(t, int32(2), calls.Load())
		})
	}
}

func TestVPCOperationsRejectsPermanentAndEmptyResponses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status     int
		body, want string
	}{
		{http.StatusNotFound, `{"message":"not found"}`, "API 404: not found"},
		{http.StatusNoContent, "", "empty response for VPC operation op1"},
		{http.StatusOK, `{}`, "empty response for VPC operation op1"},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			t.Parallel()
			v, err := NewVPC(serve(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET /v1/vpcs/operations/op1", r.Method+" "+r.URL.Path)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}), noAuth)
			require.NoError(t, err)
			_, err = v.Operations()(t.Context(), "op1")
			require.EqualError(t, err, tc.want)

			if tc.status == http.StatusNotFound {
				var ae *APIError
				require.ErrorAs(t, err, &ae)
				assert.Same(t, ae, err)
			}
		})
	}
}

func TestPollErrorClassifiesFailures(t *testing.T) {
	t.Parallel()
	require.NoError(t, PollError(nil))

	networkErr := errors.New("network failure")
	for _, tc := range []struct {
		name      string
		err       error
		transient bool
	}{
		{"400", &APIError{Status: http.StatusBadRequest}, false},
		{"429", &APIError{Status: http.StatusTooManyRequests}, true},
		{"503", &APIError{Status: http.StatusServiceUnavailable}, true},
		{"403 access verification", &APIError{Status: http.StatusForbidden, Message: "rpc: failed to verify project access: unavailable"}, true},
		{"403 denied", &APIError{Status: http.StatusForbidden, Message: "denied"}, false},
		{"non API error", networkErr, true},
		{"wrapped 400", fmt.Errorf("poll: %w", &APIError{Status: http.StatusBadRequest}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := PollError(tc.err)
			require.ErrorIs(t, got, tc.err)

			if tc.transient {
				assert.NotSame(t, tc.err, got)
				assert.Equal(t, tc.err.Error(), got.Error())
				assert.Same(t, tc.err, errors.Unwrap(got))
			} else {
				assert.Same(t, tc.err, got)
			}
		})
	}
}
