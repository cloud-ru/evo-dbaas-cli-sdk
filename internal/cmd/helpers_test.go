package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

// Общие помощники корневых тестов: фейковое облако и запуск команды.

// TestMain фиксирует язык: newApp берёт его из LC_ALL / LANG машины, а тексты
// в тестах — на en. LC_ALL перекрывает остальные, как в POSIX.
func TestMain(m *testing.M) {
	if err := os.Setenv("LC_ALL", "C"); err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

// fakeCloud — IAM и API ClickHouse на одном сервере. Обработчики API задаёт тест.
type fakeCloud struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]http.HandlerFunc // "GET /v1/clusters" → обработчик
	calls  []string
}

func newFakeCloud(t *testing.T) *fakeCloud {
	t.Helper()
	f := &fakeCloud{t: t, routes: map[string]http.HandlerFunc{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	pollDelay = time.Millisecond

	t.Setenv("CLOUDRU_IAM_AUTH_URL", srv.URL+"/iam/token")
	t.Setenv("CLOUDRU_CLICKHOUSE_API_URL", srv.URL)
	t.Setenv("CLOUDRU_REDIS_API_URL", srv.URL)
	t.Setenv("CLOUDRU_KAFKA_API_URL", srv.URL)
	t.Setenv("CLOUDRU_COMPUTE_API_URL", srv.URL)
	t.Setenv("CLOUDRU_VPC_API_URL", srv.URL)
	t.Setenv("CLOUDRU_IAM_API_URL", srv.URL)
	t.Setenv("CLOUDRU_KEY_ID", "kid")
	t.Setenv("CLOUDRU_KEY_SECRET", "sec")
	t.Setenv("CLOUDRU_PROJECT_ID", "proj-1")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	return f
}

func (f *fakeCloud) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/iam/token" {
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))

		return
	}

	if got := r.Header.Get("Authorization"); got != "Bearer tok" {
		f.t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, got)
	}

	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.calls = append(f.calls, key)
	h := f.routes[key]
	f.mu.Unlock()

	if h == nil {
		f.t.Errorf("неожиданный запрос %s", key)
		w.WriteHeader(http.StatusNotFound)

		return
	}

	h(w, r)
}

func (f *fakeCloud) on(key string, h http.HandlerFunc) { f.routes[key] = h }

func jsonReply(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runIn(t, "", args...)
}

func runIn(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	out, _, err := execute(NewDBaaSRoot(), stdin, chArgs(args)...)

	return out, err
}

// runStreams — как run, но отдаёт и stderr: туда идут сообщения.
func runStreams(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return execute(NewDBaaSRoot(), "", chArgs(args)...)
}

// chArgs — команда ClickHouse: тесты пишут её без слова clickhouse.
func chArgs(args []string) []string { return append([]string{"clickhouse"}, args...) }

func execute(root *cobra.Command, stdin string, args ...string) (stdout, stderr string, err error) {
	root.SetIn(strings.NewReader(stdin))

	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.Execute()

	return out.String(), errOut.String(), err
}

// Идентификаторы compute — UUID: CLI отказывает в другом виде до запроса.
const (
	testProject = "11111111-1111-4111-8111-111111111111"
	testSubnet  = "22222222-2222-4222-8222-222222222222"
	testZone    = "33333333-3333-4333-8333-333333333333"
	testVPC     = "44444444-4444-4444-8444-444444444444"
)

// Повторы GET в тестах — без пауз: сбой 5xx у многих сценариев намеренный.
func init() { retryDelays = []time.Duration{0, 0, 0} }

func runPaas(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	out, _, err := execute(NewDBaaSRoot(), stdin, args...)

	return out, err
}

func exitCode(err error) int { return output.ExitCode(err) }
