package base

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// fake — API: ответ по «МЕТОД /путь», запросы сохраняются для проверок.
type fake struct {
	mu      sync.Mutex
	routes  map[string]string
	bodies  map[string]string
	queries map[string]string
	calls   []string
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
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"no route"}`)

		return
	}

	_, _ = io.WriteString(w, body)
}

func (f *fake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

// run собирает state с фейками Redis и compute и отдельными буферами потоков.
func run(t *testing.T, f *fake, json bool, build func(*state.State) *cobra.Command, args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	opts := cloud.Options{
		HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil },
	}
	r, err := cloud.NewRedis(srv.URL, opts)
	require.NoError(t, err)
	c, err := cloud.NewCompute(srv.URL, opts)
	require.NoError(t, err)

	var out, errOut bytes.Buffer

	s := &state.State{
		Printer: &output.Printer{Out: &out, Err: &errOut, JSON: json}, Project: "p1",
		Timeout: time.Second, PollDelay: time.Millisecond,
		Redis:   func() (*cloud.Redis, error) { return r, nil },
		Compute: func() (*cloud.Compute, error) { return c, nil },
		Env:     func(string) string { return "" },
	}
	cmd := build(s)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err = cmd.ExecuteContext(t.Context())

	return out.String(), errOut.String(), err
}

func redisOps(s *state.State) (operation.Getter, error) {
	r, err := s.Redis()
	if err != nil {
		return nil, err
	}

	return r.Operations(), nil
}

func finishCmd(s *state.State, raw string, ops Ops) *cobra.Command {
	var wait bool

	cmd := &cobra.Command{Use: "dbctl", RunE: func(cmd *cobra.Command, _ []string) error {
		return Finish(cmd, s, []byte(raw), wait, ops, "redis operations get")
	}}
	cmd.Flags().BoolVar(&wait, "wait", false, "ждать завершения")

	return cmd
}

func TestFinishWaitPrintsFinalOperation(t *testing.T) {
	t.Parallel()

	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "статус", true: "JSON"}[json], func(t *testing.T) {
			t.Parallel()

			raw := `{"id":"op1","done":true,"resource_id":"c1","description":"готово","future":17}`
			f := newFake(map[string]string{"GET /v1/operations/op1": raw})
			out, stderr, err := run(t, f, json, func(s *state.State) *cobra.Command {
				return finishCmd(s, `{"id":"op1","done":false}`, redisOps)
			}, "--wait")
			require.NoError(t, err)
			assert.Equal(t, 0, output.ExitCode(err))

			if json {
				assert.JSONEq(t, raw, out)
				assert.Empty(t, stderr)
			} else {
				assert.Contains(t, out, "operation op1 — completed")
				assert.Contains(t, out, "resource  c1")
				assert.Contains(t, stderr, "… готово")
			}

			assert.Equal(t, []string{"GET /v1/operations/op1"}, f.requests())
		})
	}
}

func TestFinishFailedOperationReturnsCodeOneAndAPIMessage(t *testing.T) {
	t.Parallel()

	raw := `{"id":"op1","done":true,"error":{"message":"нет места"},"extra":true}`
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "статус ошибки", true: "JSON ошибки"}[json], func(t *testing.T) {
			t.Parallel()
			out, _, err := run(t, newFake(map[string]string{"GET /v1/operations/op1": raw}), json, func(s *state.State) *cobra.Command {
				return finishCmd(s, `{"id":"op1","done":false}`, redisOps)
			}, "--wait")
			require.ErrorContains(t, err, "нет места")
			assert.Equal(t, 1, output.ExitCode(err))

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Contains(t, out, "failed: нет места")
			}
		})
	}
}

func TestFinishTimeoutReturnsCodeEightAndResumeCommand(t *testing.T) {
	t.Parallel()

	initial := `{"id":"op1","done":false,"description":"исходная","extra":17}`
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "таймаут", true: "JSON исходной операции"}[json], func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"GET /v1/operations/op1": `{"id":"op1","done":false,"description":"ещё выполняется"}`})
			out, _, err := run(t, f, json, func(s *state.State) *cobra.Command {
				s.Timeout = 20 * time.Millisecond
				return finishCmd(s, initial, redisOps)
			}, "--wait")
			require.ErrorContains(t, err, "dbctl redis operations get op1 --wait")
			require.ErrorContains(t, err, "did not finish within 20ms")
			assert.Equal(t, 8, output.ExitCode(err))

			if json {
				assert.JSONEq(t, initial, out)
			} else {
				assert.Empty(t, out)
			}
		})
	}
}

func TestFinishMissingOperationCannotBeWaitedFor(t *testing.T) {
	t.Parallel()

	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "статус", true: "JSON"}[json], func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			out, _, err := run(t, f, json, func(s *state.State) *cobra.Command {
				return finishCmd(s, `{"result":"accepted"}`, nil)
			}, "--wait")
			require.ErrorContains(t, err, "API response has no operation")
			assert.Equal(t, 8, output.ExitCode(err))
			assert.Empty(t, f.requests())

			if json {
				assert.JSONEq(t, `{"result":"accepted"}`, out)
			} else {
				assert.Empty(t, out)
			}
		})
	}
}

func TestFinishWithoutWaitDoesNotConstructOperationClient(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"операция ещё идёт", `{"id":"op1","done":false}`, "operation op1 — in progress\n"},
		{"нет операции", `{}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, _, err := run(t, newFake(nil), false, func(s *state.State) *cobra.Command {
				return finishCmd(s, tc.raw, nil)
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
		})
	}
}

func TestFinishPropagatesOperationClientError(t *testing.T) {
	t.Parallel()

	want := errors.New("Redis: отсутствует ключ доступа")
	f := newFake(nil)
	out, _, err := run(t, f, false, func(s *state.State) *cobra.Command {
		s.Redis = func() (*cloud.Redis, error) { return nil, want }
		return finishCmd(s, `{"id":"op1"}`, redisOps)
	}, "--wait")
	require.ErrorIs(t, err, want)
	assert.Contains(t, err.Error(), want.Error())
	assert.Empty(t, out)
	assert.Empty(t, f.requests())
}

func TestFinishInterruptedPollPrintsOriginalJSON(t *testing.T) {
	t.Parallel()

	initial := `{"id":"op1","done":false,"extra":17}`
	out, _, err := run(t, newFake(nil), true, func(s *state.State) *cobra.Command {
		return finishCmd(s, initial, redisOps)
	}, "--wait")
	require.ErrorContains(t, err, "polling failed")
	require.ErrorContains(t, err, "dbctl redis operations get op1")
	assert.Equal(t, 8, output.ExitCode(err))
	assert.JSONEq(t, initial, out)
}

func TestAwaitCreatedReturnsResourceOrExplainsWhereToFindIt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		initial    string
		final      string
		wait       bool
		wantID     string
		wantCode   int
		wantOut    string
		wantStderr string
	}{
		{"созданный ресурс", `{"id":"op1"}`, `{"id":"op1","done":true,"resource_id":"c1"}`, true, "c1", 0, "", ""},
		{"нет resource_id", `{"id":"op1"}`, `{"id":"op1","done":true}`, true, "", 0, "operation op1 — completed", "find the cluster with dbctl redis clusters list"},
		{"без ожидания", `{"id":"op1"}`, "", false, "", 0, "operation op1 — in progress", ""},
		{"ответ без операции", `{}`, "", true, "", 8, "", ""},
		{"ожидание не удалось", `{"id":"op1"}`, "", true, "", 8, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			routes := map[string]string{}
			if tc.final != "" {
				routes["GET /v1/operations/op1"] = tc.final
			}

			out, stderr, err := run(t, newFake(routes), false, func(s *state.State) *cobra.Command {
				return &cobra.Command{Use: "dbctl", RunE: func(cmd *cobra.Command, _ []string) error {
					id, err := AwaitCreated(cmd, s, []byte(tc.initial), tc.wait, redisOps, "redis operations get", "dbctl redis clusters list")
					assert.Equal(t, tc.wantID, id)

					return err
				}}
			})
			assert.Equal(t, tc.wantCode, output.ExitCode(err))

			if tc.wantCode == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			if tc.wantOut == "" {
				assert.Empty(t, out)
			} else {
				assert.Contains(t, out, tc.wantOut)
			}

			if tc.wantStderr == "" {
				assert.Empty(t, stderr)
			} else {
				assert.Contains(t, stderr, tc.wantStderr)
				assert.NotContains(t, out, "find the cluster with")
			}
		})
	}
}

func TestPrintOperationDescribesStatusAndOptionalFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		op   operation.Operation
		want string
	}{
		{"в процессе", operation.Operation{ID: "op1"}, "operation op1 — in progress\n"},
		{"завершена", operation.Operation{ID: "op1", Done: true}, "operation op1 — completed\n"},
		{"с ошибкой", operation.Operation{ID: "op1", Done: true, Failed: true, Message: "нет места"}, "operation op1 — failed: нет места\n"},
		{"без описания ошибки", operation.Operation{ID: "op1", Done: true, Failed: true}, "operation op1 — failed\n"},
		{"ресурс и описание", operation.Operation{ID: "op1", ResourceID: "c1", Description: "создание"}, "operation op1 — in progress\nresource  c1\n          создание\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			PrintOperation(&out, tc.op)
			assert.Equal(t, tc.want, out.String())
		})
	}
}

func TestPasswordReadsFirstLineOrEnvironmentAndRejectsInvalidSources(t *testing.T) {
	t.Parallel()

	readErr := errors.New("сломанный stdin")
	for _, tc := range []struct {
		name        string
		args        []string
		env         string
		input       string
		readerErr   error
		want        string
		wantErr     string
		wantSet     bool
		newPassword bool
	}{
		{name: "из окружения", args: []string{"--password-env", "PW"}, env: "secret", want: "secret", wantSet: true},
		{name: "из stdin с CRLF", args: []string{"--password-stdin"}, input: "secret\r\nignored\n", want: "secret", wantSet: true},
		{name: "stdin без переноса", args: []string{"--password-stdin"}, input: "secret", want: "secret", wantSet: true, newPassword: true},
		{name: "две несовместимые опции", args: []string{"--password-env", "PW", "--password-stdin"}, env: "secret", input: "secret\n", wantErr: "cannot be used together", wantSet: true},
		{name: "нет источника", wantErr: "a password is required"},
		{name: "пустое окружение", args: []string{"--password-env", "PW"}, wantErr: "a password is required", wantSet: true},
		{name: "пустой stdin", args: []string{"--password-stdin"}, wantErr: "a password is required", wantSet: true},
		{name: "пустая строка CRLF", args: []string{"--password-stdin"}, input: "\r\n", wantErr: "a password is required", wantSet: true},
		{name: "ошибка чтения", args: []string{"--password-stdin"}, readerErr: readErr, wantErr: "stdin: сломанный stdin", wantSet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var password string

			_, _, err := run(t, newFake(nil), false, func(s *state.State) *cobra.Command {
				s.Env = func(name string) string {
					assert.Equal(t, "PW", name)
					return tc.env
				}

				var p Password

				cmd := &cobra.Command{Use: "password", RunE: func(cmd *cobra.Command, _ []string) error {
					assert.Equal(t, tc.wantSet, p.Set())

					var err error

					password, err = p.Read(cmd, s)

					return err
				}}
				if tc.newPassword {
					p.RegisterNew(cmd)
				} else {
					p.Register(cmd)
				}

				if tc.readerErr != nil {
					cmd.SetIn(errorReader{tc.readerErr})
				} else {
					cmd.SetIn(strings.NewReader(tc.input))
				}

				return cmd
			}, tc.args...)
			assert.Equal(t, tc.want, password)

			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Equal(t, 1, output.ExitCode(err))
			}

			if tc.readerErr != nil {
				assert.ErrorIs(t, err, tc.readerErr)
			}
		})
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestOneOfNormalizesEnumAndExplainsAllowedValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		input string
		want  string
	}{
		{"standalone", "standalone"}, {"MASTER_REPLICA", "master-replica"}, {"Master-Replica", "master-replica"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			got, err := OneOf("type", tc.input, "standalone", "master-replica")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	got, err := OneOf("type", "cluster", "standalone", "master-replica")
	require.ErrorContains(t, err, `--type "cluster": allowed values: standalone | master-replica`)
	assert.Empty(t, got)
	assert.Equal(t, 1, output.ExitCode(err))
}

func TestParseKVKeepsValuesAndLastDuplicateAndRejectsMissingKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input []string
		want  map[string]string
		bad   string
	}{
		{"пустой список", nil, map[string]string{}, ""},
		{"значения и повтор ключа", []string{"a=old", "empty=", "equals=a=b", "a=new"}, map[string]string{"a": "new", "empty": "", "equals": "a=b"}, ""},
		{"нет знака равенства", []string{"bad"}, nil, "bad"},
		{"пустой ключ", []string{"=value"}, nil, "=value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseKV("param", tc.input)
			assert.Equal(t, tc.want, got)

			if tc.bad == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "--param")
				require.ErrorContains(t, err, tc.bad)
				require.ErrorContains(t, err, "expected key=value")
				assert.Equal(t, 1, output.ExitCode(err))
			}
		})
	}
}

func TestBodyAcceptsLiteralFileOrEmptyAndReportsFileErrors(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "body.json")
	raw := []byte("{\n  \"extra\": 1\n}\n")
	require.NoError(t, os.WriteFile(path, raw, 0o600))

	for _, tc := range []struct {
		name  string
		input string
		want  []byte
		bad   bool
	}{
		{"пустое значение", "", nil, false},
		{"строка", string(raw), raw, false},
		{"файл", "@" + path, raw, false},
		{"нет файла", "@" + path + ".missing", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Body(tc.input)
			assert.Equal(t, tc.want, got)

			if tc.bad {
				require.ErrorContains(t, err, "--body:")
				require.ErrorContains(t, err, ".missing")
				assert.Equal(t, 1, output.ExitCode(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoggingRequiresEnabledFlagForExplicitGroup(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want cloud.Logging
		bad  bool
	}{
		{"не задано", nil, cloud.Logging{}, false},
		{"группа по умолчанию", []string{"--logging"}, cloud.Logging{Enabled: true}, false},
		{"выбранная группа", []string{"--logging", "--log-group-id", "lg1"}, cloud.Logging{Enabled: true, GroupID: "lg1"}, false},
		{"группа без включения", []string{"--log-group-id", "lg1"}, cloud.Logging{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := run(t, newFake(nil), false, func(_ *state.State) *cobra.Command {
				var logging Logging

				cmd := &cobra.Command{Use: "logging", RunE: func(_ *cobra.Command, _ []string) error {
					got, err := logging.Value()
					assert.Equal(t, tc.want, got)

					return err
				}}
				logging.Register(cmd)

				return cmd
			}, tc.args...)
			if tc.bad {
				require.ErrorContains(t, err, "--log-group-id requires --logging")
				assert.Equal(t, 1, output.ExitCode(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestZoneIDAcceptsIDOrFindsNameAndReportsLookupFailures(t *testing.T) {
	t.Parallel()

	clientErr := errors.New("compute: не задан ключ")
	for _, tc := range []struct {
		name      string
		value     string
		routes    map[string]string
		clientErr error
		want      string
		wantErr   string
		requests  []string
	}{
		{"id без az не требует compute", "z1", nil, clientErr, "z1", "", nil},
		{"имя без учёта регистра", "RU.az-1", map[string]string{"GET /api/v1/availability-zones": `[{"id":"z2","name":"ru.AZ-2"},{"id":"z1","name":"ru.AZ-1"}]`}, nil, "z1", "", []string{"GET /api/v1/availability-zones"}},
		{"зона не найдена", "ru.AZ-9", map[string]string{"GET /api/v1/availability-zones": `[{"id":"z1","name":"ru.AZ-1"}]`}, nil, "", "not found; see zones list", []string{"GET /api/v1/availability-zones"}},
		{"ошибка создания клиента", "ru.AZ-1", nil, clientErr, "", clientErr.Error(), nil},
		{"ошибка запроса", "ru.AZ-1", nil, nil, "", "zones: API 404", []string{"GET /api/v1/availability-zones"}},
		{"неверный ответ", "ru.AZ-1", map[string]string{"GET /api/v1/availability-zones": `{"items":[]}`}, nil, "", "cannot parse zone list", []string{"GET /api/v1/availability-zones"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(tc.routes)

			_, _, err := run(t, f, false, func(s *state.State) *cobra.Command {
				if tc.clientErr != nil {
					s.Compute = func() (*cloud.Compute, error) { return nil, tc.clientErr }
				}

				return &cobra.Command{Use: "zone", RunE: func(cmd *cobra.Command, _ []string) error {
					id, err := ZoneID(cmd.Context(), s, tc.value)
					assert.Equal(t, tc.want, id)

					return err
				}}
			})
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Equal(t, 1, output.ExitCode(err))
			}

			if tc.clientErr != nil && tc.wantErr != "" {
				require.ErrorIs(t, err, tc.clientErr)
			}

			assert.Equal(t, tc.requests, f.requests())
		})
	}
}

func TestWaitOptionsPassesDurationsAndWritesProgressToStderr(t *testing.T) {
	t.Parallel()

	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "сообщения", true: "JSON без прогресса"}[json], func(t *testing.T) {
			t.Parallel()

			var out, stderr bytes.Buffer

			s := &state.State{Timeout: 3 * time.Second, PollDelay: 7 * time.Millisecond, Printer: &output.Printer{Out: &out, Err: &stderr, JSON: json}}
			opts := WaitOptions(s)
			assert.Equal(t, s.Timeout, opts.Timeout)
			assert.Equal(t, s.PollDelay, opts.Delay)
			opts.Progress("создание")
			opts.Retry(errors.New("сеть недоступна"))
			assert.Empty(t, out.String())

			if json {
				assert.Empty(t, stderr.String())
			} else {
				assert.Contains(t, stderr.String(), "… создание")
				assert.Contains(t, stderr.String(), "… polling failed (сеть недоступна), retrying")
			}
		})
	}
}

func TestListAndDescribeRespectArgumentsAliasesAndOutputMode(t *testing.T) {
	t.Parallel()

	raw := `{"versions":[{"id":"v7","extra":17}]}`
	for _, describe := range []bool{false, true} {
		for _, json := range []bool{false, true} {
			name := map[bool]string{false: "list", true: "describe"}[describe] + map[bool]string{false: " таблица", true: " JSON"}[json]
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				f := newFake(map[string]string{"GET /v1/versions": raw})

				var tableCalled bool

				out, _, err := run(t, f, json, func(s *state.State) *cobra.Command {
					c := ListCmd{
						Use: "list ID", Short: "версии", Aliases: []string{"ls"}, Args: cobra.ExactArgs(1),
						List: func(ctx context.Context, got *state.State, args []string) ([]byte, error) {
							assert.Same(t, s, got)
							assert.Equal(t, []string{"v7"}, args)

							r, err := got.Redis()
							if err != nil {
								return nil, err
							}

							return r.Versions(ctx)
						},
						Table: func(w io.Writer, got []byte) error {
							tableCalled = true

							assert.Equal(t, raw, string(got))

							_, err := io.WriteString(w, "версия v7\n")

							require.NoError(t, err)

							return nil
						},
					}

					var sub *cobra.Command
					if describe {
						sub = DescribeCmd(c).Cmd(s)
					} else {
						sub = c.Cmd(s)
					}

					assert.Equal(t, c.Short, sub.Short)
					assert.Equal(t, c.Aliases, sub.Aliases)

					root := &cobra.Command{Use: "dbctl"}
					root.AddCommand(sub)

					return root
				}, "ls", "v7")
				require.NoError(t, err)
				assert.Equal(t, !json, tableCalled)

				if json {
					assert.JSONEq(t, raw, out)
				} else {
					assert.Equal(t, "версия v7\n", out)
				}
			})
		}
	}
}

func TestListPropagatesCallTableAndOutputErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("проверяемая ошибка")
	for _, mode := range []string{"call", "table", "output", "args"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			called := false

			args := []string{"v7"}
			if mode == "args" {
				args = nil
			}

			_, _, err := run(t, newFake(nil), mode == "output", func(s *state.State) *cobra.Command {
				if mode == "output" {
					s.Printer.Out = errorWriter{want}
				}

				return (ListCmd{
					Use: "list ID", Args: cobra.ExactArgs(1),
					List: func(context.Context, *state.State, []string) ([]byte, error) {
						called = true

						if mode == "call" {
							return nil, want
						}

						return []byte(`{}`), nil
					},
					Table: func(io.Writer, []byte) error { return want },
				}).Cmd(s)
			}, args...)
			if mode == "args" {
				require.Error(t, err)
				assert.False(t, called)
			} else {
				require.ErrorIs(t, err, want)
				assert.True(t, called)
			}
		})
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestDeleteConfirmsMutationWaitsWhenRequestedAndPropagatesErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		args     []string
		routes   map[string]string
		noWait   bool
		wantErr  string
		wantCode int
		wantOut  string
		requests []string
	}{
		{"нет подтверждения", []string{"delete", "c1"}, nil, false, "rerun with --yes", 1, "", nil},
		{"ошибка DELETE", []string{"delete", "c1", "--yes"}, nil, false, "API 404", 1, "", []string{"DELETE /v1/clusters/c1"}},
		{"операция без ожидания", []string{"delete", "c1", "--yes"}, map[string]string{"DELETE /v1/clusters/c1": `{"id":"op1"}`}, false, "", 0, "operation op1 — in progress", []string{"DELETE /v1/clusters/c1"}},
		{"операция с ожиданием", []string{"delete", "c1", "--yes", "--wait"}, map[string]string{"DELETE /v1/clusters/c1": `{"id":"op1"}`, "GET /v1/operations/op1": `{"id":"op1","done":true}`}, false, "", 0, "operation op1 — completed", []string{"DELETE /v1/clusters/c1", "GET /v1/operations/op1"}},
		{"синхронное удаление", []string{"delete", "c1", "--yes"}, map[string]string{"DELETE /v1/clusters/c1": `{}`}, true, "", 0, "", []string{"DELETE /v1/clusters/c1"}},
		{"синхронное удаление не принимает wait", []string{"delete", "c1", "--yes", "--wait"}, nil, true, "unknown flag: --wait", 1, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(tc.routes)
			out, _, err := run(t, f, false, func(s *state.State) *cobra.Command {
				sub := (DeleteCmd{
					Use: "delete ID", Args: cobra.ExactArgs(1), Ops: redisOps, OpsCmd: "redis operations get", NoWait: tc.noWait,
					Delete: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
						r, err := s.Redis()
						if err != nil {
							return nil, err
						}

						return r.DeleteCluster(ctx, args[0])
					},
				}).Cmd(s)
				if tc.noWait {
					assert.Nil(t, sub.Flags().Lookup("wait"))
				} else {
					assert.NotNil(t, sub.Flags().Lookup("wait"))
				}

				root := &cobra.Command{Use: "dbctl"}
				root.AddCommand(sub)

				return root
			}, tc.args...)
			assert.Equal(t, tc.wantCode, output.ExitCode(err))

			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}

			if tc.wantOut == "" {
				assert.Empty(t, out)
			} else {
				assert.Contains(t, out, tc.wantOut)
			}

			assert.Equal(t, tc.requests, f.requests())
		})
	}
}
