package cluster

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// fake — API Redis: ответ по «МЕТОД /путь», тела запросов — для проверки.
type fake struct {
	mu     sync.Mutex
	routes map[string]string
	bodies map[string]string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.bodies[key] = string(b)
	body, ok := f.routes[key]
	f.mu.Unlock()

	if !ok {
		http.Error(w, `{"message":"no route"}`, http.StatusNotFound)

		return
	}

	_, _ = w.Write([]byte(body))
}

// run выполняет redis clusters с аргументами против фейка; stdout — вывод Printer.
func run(t *testing.T, f *fake, json bool, args ...string) (string, error) {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	r, err := cloud.NewRedis(srv.URL, cloud.Options{
		HTTPClient: srv.Client(),
		Auth:       func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	var out, errOut bytes.Buffer

	s := &state.State{
		Printer: &output.Printer{Out: &out, Err: &errOut, JSON: json}, Project: "p1",
		Timeout: time.Second, PollDelay: time.Millisecond,
		Redis: func() (*cloud.Redis, error) { return r, nil },
	}

	cmd := NewCmd(s)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err = cmd.ExecuteContext(t.Context())

	return out.String(), err
}

func newFake(routes map[string]string) *fake {
	return &fake{routes: routes, bodies: map[string]string{}}
}

func TestListTable(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"GET /v1/clusters": `{"clusters":[{"id":"c1","name":"cache","status":"READY","type":"STANDALONE","shards":1,"replicas":1,"storage_gib":15}]}`})

	out, err := run(t, f, false, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "c1")
	assert.Contains(t, out, "cache")
	assert.Contains(t, out, "STANDALONE")
	assert.Contains(t, out, "15")
}

func TestListJSONAsIs(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"GET /v1/clusters": `{"clusters":[{"id":"c1","extra":1}]}`})

	out, err := run(t, f, true, "list")
	require.NoError(t, err)
	assert.JSONEq(t, `{"clusters":[{"id":"c1","extra":1}]}`, out)
}

func TestListNeedsProject(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(newFake(nil))
	t.Cleanup(srv.Close)

	s := &state.State{Printer: &output.Printer{Out: io.Discard, Err: io.Discard}}
	cmd := NewCmd(s)
	cmd.SetArgs([]string{"list"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.ErrorIs(t, cmd.ExecuteContext(t.Context()), state.ErrNoProject)
}

func TestGetPrintsFields(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"GET /v1/clusters/c1": `{"id":"c1","name":"cache","redis_hosts":["10.0.0.1","10.0.0.2"],"subnet_ids":["sn1"],"created_at":"2026-10-26T10:00:00Z"}`})

	out, err := run(t, f, false, "get", "c1")
	require.NoError(t, err)
	assert.Contains(t, out, "10.0.0.1, 10.0.0.2")
	assert.Contains(t, out, "sn1")
	assert.Contains(t, out, "2026-10-26T10:00:00Z")
}

// Без проекта create отказывает до запроса: кластер без project_id API не создаст.
func TestCreateNeedsProject(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"POST /v1/clusters": `{"id":"op1","done":false}`})
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	r, err := cloud.NewRedis(srv.URL, cloud.Options{
		HTTPClient: srv.Client(),
		Auth:       func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	s := &state.State{
		Printer: &output.Printer{Out: io.Discard, Err: io.Discard},
		Redis:   func() (*cloud.Redis, error) { return r, nil },
	}
	cmd := NewCmd(s)
	cmd.SetArgs([]string{"create", "--name", "n", "--version-id", "v", "--spec", "s"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.ErrorIs(t, cmd.ExecuteContext(t.Context()), state.ErrNoProject)
	assert.Empty(t, f.bodies)
}

// create --wait: сбой операции и сбой чтения созданного кластера доходят до
// пользователя с кодом 1, а не теряются и не подменяются запросом кластера.
func TestCreateWaitReportsFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		routes  map[string]string
		want    string
		fetched bool
	}{
		{"операция завершилась ошибкой", map[string]string{
			"POST /v1/clusters":      `{"id":"op1","done":false}`,
			"GET /v1/operations/op1": `{"id":"op1","done":true,"resource_id":"c1","error":{"code":9,"message":"busy"}}`,
			"GET /v1/clusters/c1":    `{"id":"c1"}`,
		}, "operation op1 failed: busy", false},
		{"кластер не прочитан", map[string]string{
			"POST /v1/clusters":      `{"id":"op1","done":false}`,
			"GET /v1/operations/op1": `{"id":"op1","done":true,"resource_id":"c1"}`,
		}, "API 404: no route", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(tc.routes)
			_, err := run(t, f, false, "create", "--name", "n", "--version-id", "v", "--spec", "s", "--wait")
			require.ErrorContains(t, err, tc.want)
			assert.Equal(t, 1, output.ExitCode(err))

			_, fetched := f.bodies["GET /v1/clusters/c1"]
			assert.Equal(t, tc.fetched, fetched)
		})
	}
}

func TestCreateWaitPrintsCluster(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{
		"POST /v1/clusters":      `{"id":"op1","done":false,"resource_id":"c1"}`,
		"GET /v1/operations/op1": `{"id":"op1","done":true,"resource_id":"c1"}`,
		"GET /v1/clusters/c1":    `{"id":"c1","name":"n","status":"READY"}`,
	})

	out, err := run(t, f, false, "create", "--name", "n", "--version-id", "v", "--spec", "s",
		"--type", "MASTER_REPLICA", "--param", "maxmemory-policy=noeviction", "--logging", "--wait")
	require.NoError(t, err)
	assert.Contains(t, out, "READY")

	body := f.bodies["POST /v1/clusters"]
	assert.Contains(t, body, `"type":"MASTER_REPLICA"`)
	assert.Contains(t, body, `"project_id":"p1"`)
	assert.Contains(t, body, `"maxmemory-policy":"noeviction"`)
	assert.Contains(t, body, `"logging":{"enabled":true}`)
}

// Необязательные поля уходят в запрос, только если флаг задан: storage_gib: 0
// или пустой subnet_ids API трактует как значение, а не как «не указано».
func TestCreateSendsOnlyGivenOptionals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"без необязательных", nil, `{"project_id":"p1","name":"n","version_id":"v","specification_id":"s","type":"STANDALONE","shards":1,"replicas":1}`},
		{"описание", []string{"--description", "кэш"}, `{"project_id":"p1","name":"n","description":"кэш","version_id":"v","specification_id":"s","type":"STANDALONE","shards":1,"replicas":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"POST /v1/clusters": `{"id":"op1","done":false}`})
			_, err := run(t, f, false, append([]string{"create", "--name", "n", "--version-id", "v", "--spec", "s"}, tc.args...)...)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, f.bodies["POST /v1/clusters"])
		})
	}
}

func TestCreateRejectsBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"тип", []string{"--type", "cluster"}, "allowed values: standalone | master-replica"},
		{"тип unspecified", []string{"--type", "REDIS_CLUSTER_TYPE_UNSPECIFIED"}, "allowed values: standalone | master-replica"},
		{"параметр", []string{"--param", "bad"}, "key=value"},
		{"лог-группа без логов", []string{"--log-group-id", "lg"}, "requires --logging"},
		{"body из файла", []string{"--body", "@/нет/такого"}, "--body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			_, err := run(t, f, false, append([]string{"create", "--name", "n", "--version-id", "v", "--spec", "s"}, tc.args...)...)
			require.ErrorContains(t, err, tc.want)
			assert.Empty(t, f.bodies)
		})
	}
}

func TestResize(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"PATCH /v1/clusters/c1": `{"id":"op2","done":false}`})

	out, err := run(t, f, false, "resize", "c1", "--storage-gib", "30", "--body", `{"x":1}`)
	require.NoError(t, err)
	assert.Contains(t, out, "op2")
	assert.Contains(t, f.bodies["PATCH /v1/clusters/c1"], `"storage_gib":30`)
	assert.Contains(t, f.bodies["PATCH /v1/clusters/c1"], `"x":1`)

	for _, args := range [][]string{{"resize", "c1"}, {"resize", "c1", "--spec", "s2", "--storage-gib", "30"}, {"resize", "c1", "--param", "x"}} {
		_, err := run(t, newFake(nil), false, args...)
		require.Error(t, err, strings.Join(args, " "))
	}

	// API: 400 «conflicting update fields» — отказ до запроса.
	f = newFake(nil)
	_, err = run(t, f, false, "resize", "c1", "--spec", "s2", "--storage-gib", "30")
	require.ErrorContains(t, err, "separate calls")
	assert.Empty(t, f.bodies)
}

// --wait упёрся в --timeout: подсказка ведёт к операциям Redis, а не другого продукта.
func TestDeleteWaitTimeoutHintsResume(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{
		"DELETE /v1/clusters/c1": `{"id":"op1","done":false}`,
		"GET /v1/operations/op1": `{"id":"op1","done":false}`,
	})

	_, err := run(t, f, false, "delete", "c1", "--yes", "--wait")
	require.ErrorContains(t, err, " redis operations get op1 --wait`")
	assert.Equal(t, 8, output.ExitCode(err))
}

// Операция удаления завершилась ошибкой: код 1 и причина из API, а не 0 или 8.
func TestDeleteWaitFailedOperationExitCode1(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{
		"DELETE /v1/clusters/c1": `{"id":"op2","done":false}`,
		"GET /v1/operations/op2": `{"id":"op2","done":true,"error":{"code":9,"message":"busy"}}`,
	})

	_, err := run(t, f, false, "delete", "c1", "--yes", "--wait")
	require.ErrorContains(t, err, "operation op2 failed: busy")
	assert.Equal(t, 1, output.ExitCode(err))
}

func TestDeleteNeedsYes(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"DELETE /v1/clusters/c1": `{"id":"op3","done":false}`})

	_, err := run(t, f, false, "delete", "c1")
	require.ErrorContains(t, err, "--yes")
	assert.Empty(t, f.bodies)

	out, err := run(t, f, false, "delete", "c1", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "op3")
}

func TestAPIErrorSurfaced(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"create", "--name", "n", "--version-id", "v", "--spec", "s"},
		{"resize", "c1", "--storage-gib", "30"},
	} {
		_, err := run(t, newFake(nil), false, args...)
		require.ErrorContains(t, err, "API 404: no route", args[0])
	}
}
