package cluster

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	mu     sync.Mutex
	routes map[string]string
	bodies map[string]string
	calls  []string
}

func newFake(routes map[string]string) *fake {
	return &fake{routes: routes, bodies: map[string]string{}}
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.bodies[key] = string(b)
	f.calls = append(f.calls, key)
	body, ok := f.routes[key]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"no route"}`)

		return
	}

	_, _ = io.WriteString(w, body)
}

func (f *fake) body(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bodies[key]
}

func (f *fake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

// run собирает команду с настоящим клиентом Kafka и буфером вывода.
func run(t *testing.T, f *fake, json bool, args ...string) (string, error) {
	t.Helper()
	out, _, err := runWith(t, f, json, nil, args...)

	return out, err
}

// runWith позволяет задать окружение, stdin или ошибку создания клиента.
func runWith(t *testing.T, f *fake, json bool, setup func(*state.State, *cobra.Command), args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	r, err := cloud.NewKafka(srv.URL, cloud.Options{
		HTTPClient: srv.Client(),
		Auth:       func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	var out, errOut bytes.Buffer

	s := &state.State{
		Printer: &output.Printer{Out: &out, Err: &errOut, JSON: json}, Project: "p1",
		Timeout: time.Second, PollDelay: time.Millisecond,
		Env:   func(string) string { return "" },
		Kafka: func() (*cloud.Kafka, error) { return r, nil },
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

func TestListPrintsTableAndPreservesJSON(t *testing.T) {
	t.Parallel()

	raw := `{"clusters":[{"id":"c1","name":"events","status":"READY","health":"ALIVE","brokers":3,"storage_gib":40,"future":12345678901234567890}]}`
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "таблица", true: "JSON"}[json], func(t *testing.T) {
			t.Parallel()

			out, err := run(t, newFake(map[string]string{"GET /v1/clusters": raw}), json, "list")
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Equal(t, []string{"ID", "NAME", "STATUS", "HEALTH", "BROKERS", "STORAGE_GIB", "c1", "events", "READY", "ALIVE", "3", "40"}, strings.Fields(out))
			}
		})
	}
}

func TestGetPrintsHostsAndOptionalFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, raw string
		fields    []string
	}{
		{"все поля", `{"id":"c1","name":"events","status":"READY","health":"ALIVE","version_id":"v1","specification_id":"s1","brokers":3,"storage_gib":40,"kafka_hosts":["a:9092","b:9092"],"kafka_ui_host":"ui.example","created_at":"2026-10-26T12:00:00Z","future":42}`, []string{"hosts", "a:9092, b:9092", "ui_host", "ui.example", "created_at", "2026-10-26T12:00:00Z"}},
		{"без необязательных полей", `{"id":"c1","kafka_ui_host":""}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, json := range []bool{false, true} {
				out, err := run(t, newFake(map[string]string{"GET /v1/clusters/c1": tc.raw}), json, "get", "c1")
				require.NoError(t, err)

				if json {
					assert.JSONEq(t, tc.raw, out)
					continue
				}

				for _, field := range tc.fields {
					assert.Contains(t, out, field)
				}

				if tc.fields == nil {
					for _, field := range []string{"hosts", "ui_host", "created_at"} {
						assert.NotContains(t, out, field)
					}
				}
			}
		})
	}
}

func TestCreateWaitPrintsClusterAndSendsFlags(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{
		"POST /v1/clusters":      `{"id":"op1","done":false,"resource_id":"c1"}`,
		"GET /v1/operations/op1": `{"id":"op1","done":true,"resource_id":"c1"}`,
		"GET /v1/clusters/c1":    `{"id":"c1","name":"events","status":"READY"}`,
	})
	out, err := run(t, f, false, "create", "--name", "events", "--version-id", "v1", "--spec", "s1",
		"--storage-gib", "40", "--subnet-id", "sn1", "--brokers", "3", "--ui", "--cruise-control", "--auto-rebalance",
		"--description", "описание", "--param", "retention.ms=1000", "--logging", "--log-group-id", "lg1", "--body", `{"future":true}`, "--wait")
	require.NoError(t, err)
	assert.Contains(t, out, "READY")
	assert.JSONEq(t, `{"project_id":"p1","name":"events","description":"описание","version_id":"v1","specification_id":"s1","storage_gib":40,"subnet_id":"sn1","brokers":3,"ui_enabled":true,"cruise_control_enabled":true,"cruise_control_rebalance_auto_enabled":true,"parameters":{"retention.ms":"1000"},"logging":{"enabled":true,"log_group_id":"lg1"},"future":true}`, f.body("POST /v1/clusters"))
	assert.Equal(t, []string{"POST /v1/clusters", "GET /v1/operations/op1", "GET /v1/clusters/c1"}, f.requests())
}

// Без необязательных флагов: brokers по умолчанию 1, description, parameters и logging не отправляются.
func TestCreateSendsDefaultsWithoutOptionalFlags(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"POST /v1/clusters": `{"id":"op1","done":false}`})
	_, err := run(t, f, false, "create", "--name", "events", "--version-id", "v1", "--spec", "s1", "--storage-gib", "40", "--subnet-id", "sn1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"project_id":"p1","name":"events","version_id":"v1","specification_id":"s1","storage_gib":40,"subnet_id":"sn1","brokers":1,"ui_enabled":false,"cruise_control_enabled":false,"cruise_control_rebalance_auto_enabled":false}`, f.body("POST /v1/clusters"))
}

// Каждый обязательный флаг проверяется отдельно до первого запроса.
func TestCreateRequiresFlagsBeforeRequest(t *testing.T) {
	t.Parallel()

	flags := []string{"name", "version-id", "spec", "storage-gib", "subnet-id"}
	for _, missing := range flags {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()

			args := []string{"create"}
			for _, flag := range flags {
				if flag != missing {
					value := "id1"
					if flag == "storage-gib" {
						value = "40"
					}

					args = append(args, "--"+flag, value)
				}
			}

			f := newFake(nil)
			_, err := run(t, f, false, args...)
			require.ErrorContains(t, err, missing)
			assert.Empty(t, f.requests())
		})
	}
}

func TestCreateAndResizeRejectInvalidFlagsBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"create параметр", []string{"create", "--param", "bad"}, "key=value"},
		{"create логи", []string{"create", "--log-group-id", "lg1"}, "requires --logging"},
		{"create файл", []string{"create", "--body", "@/нет/такого"}, "--body"},
		{"resize без изменений", []string{"resize", "c1"}, "specify what to change"},
		{"resize параметр", []string{"resize", "c1", "--param", "bad"}, "key=value"},
		{"resize файл", []string{"resize", "c1", "--body", "@/нет/такого"}, "--body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string(nil), tc.args...)
			if args[0] == "create" {
				args = append(args, "--name", "events", "--version-id", "v1", "--spec", "s1", "--storage-gib", "40", "--subnet-id", "sn1")
			}

			f := newFake(nil)
			_, err := run(t, f, false, args...)
			require.ErrorContains(t, err, tc.want)
			assert.Empty(t, f.requests())
		})
	}
}

func TestResizeSendsChangesAndPrintsOperation(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"PATCH /v1/clusters/c1": `{"id":"op1","done":false}`})
	out, err := run(t, f, false, "resize", "c1", "--spec", "s2", "--storage-gib", "80", "--param", "x=y", "--body", `{"future":1}`)
	require.NoError(t, err)
	assert.Contains(t, out, "op1")
	assert.JSONEq(t, `{"cluster_id":"c1","specification_id":"s2","storage_gib":80,"parameters":{"x":"y"},"future":1}`, f.body("PATCH /v1/clusters/c1"))
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"DELETE /v1/clusters/c1": `{"id":"op1","done":false}`})
	_, err := run(t, f, false, "delete", "c1")
	require.ErrorContains(t, err, "--yes")
	assert.Empty(t, f.requests())

	out, err := run(t, f, false, "delete", "c1", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "op1")
	assert.Equal(t, []string{"DELETE /v1/clusters/c1"}, f.requests())
}

func TestListAndCreateRequireProjectBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"list"},
		{"create", "--name", "events", "--version-id", "v1", "--spec", "s1", "--storage-gib", "40", "--subnet-id", "sn1"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) { s.Project = "" }, args...)
			require.ErrorIs(t, err, state.ErrNoProject)
			assert.Empty(t, f.requests())
		})
	}
}

func TestCommandsPropagateClientErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("Kafka недоступен")
	for _, args := range [][]string{
		{"list"},
		{"get", "c1"},
		{"delete", "c1", "--yes"},
		{"resize", "c1", "--spec", "s2"},
		{"create", "--name", "events", "--version-id", "v1", "--spec", "s1", "--storage-gib", "40", "--subnet-id", "sn1"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) {
				s.Kafka = func() (*cloud.Kafka, error) { return nil, want }
			}, args...)
			require.ErrorIs(t, err, want)
			assert.Empty(t, f.requests())
		})
	}
}

func TestCommandsReportAPIAndDecodeErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		args   []string
		routes map[string]string
		want   string
	}{
		{"list API", []string{"list"}, nil, "API 404"},
		{"get API", []string{"get", "c1"}, nil, "API 404"},
		{"delete API", []string{"delete", "c1", "--yes"}, nil, "API 404"},
		{"resize API", []string{"resize", "c1", "--spec", "s2"}, nil, "API 404"},
		{"list схема", []string{"list"}, map[string]string{"GET /v1/clusters": `{"clusters":[{"brokers":"bad"}]}`}, "parse response"},
		{"get дата", []string{"get", "c1"}, map[string]string{"GET /v1/clusters/c1": `{"created_at":"bad"}`}, "parse response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := run(t, newFake(tc.routes), false, tc.args...)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestCreatePrintsOperationOrReportsWaitAndFetchErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, post    string
		wait          bool
		want, failure string
	}{
		{"без ожидания", `{"id":"op1","done":false}`, false, "op1", ""},
		{"без resource_id", `{"id":"op1","done":true}`, true, "completed", ""},
		{"ошибка POST", "", false, "", "API 404"},
		{"ошибка ожидания", `{"id":"op1","done":true,"error":{"message":"failed"}}`, true, "", "failed"},
		{"ошибка получения кластера", `{"id":"op1","done":true,"resource_id":"missing"}`, true, "", "API 404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			routes := map[string]string{}
			if tc.post != "" {
				routes["POST /v1/clusters"] = tc.post
			}

			args := []string{"create", "--name", "events", "--version-id", "v1", "--spec", "s1", "--storage-gib", "40", "--subnet-id", "sn1"}
			if tc.wait {
				args = append(args, "--wait")
			}

			out, err := run(t, newFake(routes), false, args...)
			if tc.failure != "" {
				require.ErrorContains(t, err, tc.failure)
			} else {
				require.NoError(t, err)
				assert.Contains(t, out, tc.want)
			}
		})
	}
}

func TestDeleteWaitTimeoutHintsResume(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{
		"DELETE /v1/clusters/c1": `{"id":"op1","done":false}`,
		"GET /v1/operations/op1": `{"id":"op1","done":false}`,
	})

	_, _, err := runWith(t, f, false, func(s *state.State, _ *cobra.Command) { s.Timeout = 30 * time.Millisecond },
		"delete", "c1", "--yes", "--wait")
	require.ErrorContains(t, err, " kafka operations get op1 --wait`")
}
