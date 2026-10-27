package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// fake — API: ответ по «МЕТОД /путь», запросы сохраняются для проверок.
type fake struct {
	mu       sync.Mutex
	routes   map[string]string
	statuses map[string]int
	queries  map[string]string
	bodies   map[string]string
	calls    []string
}

func newFake(routes map[string]string) *fake {
	return &fake{routes: routes, bodies: map[string]string{}, queries: map[string]string{}}
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.bodies[key] = string(b)
	f.queries[key] = r.URL.RawQuery
	f.calls = append(f.calls, key)
	body, ok := f.routes[key]
	status := f.statuses[key]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"no route"}`)

		return
	}

	if status != 0 {
		w.WriteHeader(status)
	}

	_, _ = io.WriteString(w, body)
}

func (f *fake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

// runWith позволяет задать окружение, stdin или ошибку создания клиента.
func runWith(t *testing.T, f *fake, json bool, setup func(*state.State, *cobra.Command), args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	r, err := cloud.NewClickHouse(srv.URL, cloud.Options{
		HTTPClient: srv.Client(),
		Auth:       func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	var out, errOut bytes.Buffer

	s := &state.State{
		Printer: &output.Printer{Out: &out, Err: &errOut, JSON: json}, Project: "p1",
		Timeout: time.Second, PollDelay: time.Millisecond,
		Env:        func(string) string { return "" },
		ClickHouse: func() (*cloud.ClickHouse, error) { return r, nil },
	}

	cmd := NewCmd(s)
	if setup != nil {
		setup(s, cmd)
	}

	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err = cmd.ExecuteContext(t.Context())

	return out.String(), errOut.String(), err
}

func (f *fake) query(key string) string { f.mu.Lock(); defer f.mu.Unlock(); return f.queries[key] }

func TestCheckProbesTokenAndOptionalProject(t *testing.T) {
	t.Parallel()

	for _, project := range []string{"", "p1"} {
		for _, json := range []bool{false, true} {
			f := newFake(map[string]string{"GET /v1/versions": `{"versions":[]}`, "GET /v1/clusters": `{"clusters":[]}`})
			out, stderr, err := runWith(t, f, json, func(s *state.State, _ *cobra.Command) {
				s.Project = project
				s.ClickHouseURL = "https://clickhouse.example"
			}, "check")
			require.NoError(t, err)

			want := []string{"GET /v1/versions"}
			if project != "" {
				want = append(want, "GET /v1/clusters")
				q, err := url.ParseQuery(f.query("GET /v1/clusters"))
				require.NoError(t, err)
				assert.Equal(t, url.Values{"page_size": {"1"}, "project_id": {"p1"}}, q)
			}

			assert.Equal(t, want, f.requests())
			assert.Empty(t, f.query("GET /v1/versions"))

			if json {
				assert.JSONEq(t, fmt.Sprintf(`{"ok":true,"api_url":"https://clickhouse.example","project_id":%q,"project_checked":%v}`, project, project != ""), out)
				assert.Empty(t, stderr)
			} else {
				assert.Equal(t, "access key works: https://clickhouse.example\n", out)

				if project == "" {
					assert.Contains(t, stderr, "project is not set: CLOUDRU_PROJECT_ID or --project")
				} else {
					assert.Empty(t, stderr)
				}
			}
		}
	}
}

func TestCheckPropagatesInitializationErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("client unavailable")
	f := newFake(nil)
	_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
		s.ClickHouse = func() (*cloud.ClickHouse, error) { return nil, want }
	}, "check")
	require.ErrorIs(t, err, want)
	assert.Empty(t, f.requests())
}

func TestCheckExplainsOnlyProjectVerification403(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, route, message string
		status, code         int
		hint                 bool
	}{
		{"versions failure", "GET /v1/versions", "token rejected", 401, 4, false},
		{"verification failure", "GET /v1/clusters", "failed to verify project access: unavailable", 403, 1, true},
		{"other forbidden", "GET /v1/clusters", "permission denied", 403, 4, false},
		{"server failure", "GET /v1/clusters", "failed to verify project access", 500, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"GET /v1/versions": `{}`, "GET /v1/clusters": `{}`})
			f.routes[tc.route] = fmt.Sprintf(`{"message":%q}`, tc.message)
			f.statuses = map[string]int{tc.route: tc.status}
			out, stderr, err := runWith(t, f, false, nil, "check")
			require.ErrorContains(t, err, tc.message)
			assert.Equal(t, tc.code, output.ExitCode(err))
			assert.Empty(t, out)
			assert.Empty(t, stderr)

			if tc.hint {
				assert.Contains(t, err.Error(), "platform.project.user")
				assert.Contains(t, err.Error(), "p1")
			} else {
				var ae *cloud.APIError
				require.ErrorAs(t, err, &ae)
				assert.Equal(t, tc.status, ae.Status)
				assert.Equal(t, tc.message, ae.Message)
				assert.NotContains(t, err.Error(), "platform.project.user")
			}

			want := []string{"GET /v1/versions"}
			if tc.route != "GET /v1/versions" {
				want = append(want, "GET /v1/clusters")
			}

			assert.Equal(t, want, f.requests())
		})
	}
}

func TestProjectProbeInitializationError(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"GET /v1/versions": `{}`})
	want := errors.New("second initialization failed")
	_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
		client := s.ClickHouse
		n := 0
		s.ClickHouse = func() (*cloud.ClickHouse, error) {
			n++
			if n == 2 {
				return nil, want
			}

			return client()
		}
	}, "check")
	require.ErrorIs(t, err, want)
	assert.Equal(t, []string{"GET /v1/versions"}, f.requests())
}
