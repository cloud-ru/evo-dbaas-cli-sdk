package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// run собирает команду с настоящим клиентом PostgreSQL и буфером вывода.
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
	r, err := cloud.NewPostgreSQL(srv.URL, cloud.Options{
		HTTPClient: srv.Client(),
		Auth:       func(context.Context, *http.Request) error { return nil },
	})
	require.NoError(t, err)

	var out, errOut bytes.Buffer

	s := &state.State{
		Printer: &output.Printer{Out: &out, Err: &errOut, JSON: json}, Project: "p1",
		Timeout: time.Second, PollDelay: time.Millisecond,
		Env:        func(string) string { return "" },
		PostgreSQL: func() (*cloud.PostgreSQL, error) { return r, nil },
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

// Полная матрица ловит принятие недопустимой топологии и отказ на допустимой;
// сетка e2e проверяет только типовые вызовы нового продукта.
func TestCreateDeploymentRules(t *testing.T) {
	t.Parallel()
	// Строки — 0..4 узла, столбцы — 0..3 подсети. Ожидания заданы
	// явно, независимо от порядка проверок в checkTopology.
	standard := [5][4]string{
		{"at least 1 instance", "at least 1 instance", "at least 1 instance", "at least 1 instance"},
		{"at least one subnet", "", "only available in business mode", "only available in business mode"},
		{"at least one subnet", "", "only available in business mode", "only available in business mode"},
		{"standard: maximum instances: 2", "standard: maximum instances: 2", "standard: maximum instances: 2", "standard: maximum instances: 2"},
		{"standard: maximum instances: 2", "standard: maximum instances: 2", "standard: maximum instances: 2", "standard: maximum instances: 2"},
	}
	business := [5][4]string{
		{"at least 1 instance", "at least 1 instance", "at least 1 instance", "at least 1 instance"},
		{"at least one subnet", "", "counts must match", "counts must match"},
		{"at least one subnet", "", "", "counts must match"},
		{"at least one subnet", "", "counts must match", ""},
		{"business: maximum instances: 3", "business: maximum instances: 3", "business: maximum instances: 3", "business: maximum instances: 3"},
	}

	type testCase struct {
		name, mode, lookup, want string
		max, instances, subnets  int
	}

	var cases []testCase
	for _, spec := range []struct {
		mode     string
		max      int
		expected [5][4]string
	}{
		{"STANDARD", 2, standard}, {"BUSINESS", 3, business},
	} {
		for instances := range 5 {
			for subnets := range 4 {
				for _, lookup := range []string{"found", "missing", "failed"} {
					want := spec.expected[instances][subnets]
					if lookup == "missing" {
						want = "no such specification"
					}

					if lookup == "failed" {
						want = business[instances][subnets]
					}

					cases = append(cases, testCase{fmt.Sprintf("%s/%d-instances/%d-subnets/%s", spec.mode, instances, subnets, lookup), spec.mode, lookup, want, spec.max, instances, subnets})
				}
			}
		}
	}

	cases = append(cases,
		testCase{"STANDARD/max-hosts-0/3-instances", "STANDARD", "found", "standard: maximum instances: 2", 0, 3, 1},
		testCase{"STANDARD/max-hosts-3/3-instances", "STANDARD", "found", "standard: maximum instances: 2", 3, 3, 1},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			specs := fmt.Sprintf(`{"specifications":[{"id":"s1","deployment_mode":%q,"max_hosts":%d}]}`, tc.mode, tc.max)

			routes := map[string]string{"POST /v1/clusters": `{"id":"op1"}`}
			switch tc.lookup {
			case "found":
				routes["GET /v1/specifications"] = specs
			case "missing":
				routes["GET /v1/specifications"] = `{"specifications":[]}`
			}

			f := newFake(routes)
			args := []string{"create", "--name", "demo", "--version", "17", "--spec", "s1", "--initial-database", "app", "--storage-gib", "50", "--instances", strconv.Itoa(tc.instances), "--subnet-id", strings.Join([]string{"sn1", "sn2", "sn3"}[:tc.subnets], ",")}

			out, stderr, err := runWith(t, f, false, nil, args...)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.Empty(t, out)
				assert.Equal(t, []string{"GET /v1/specifications"}, f.requests())
			} else {
				require.NoError(t, err)
				assert.Contains(t, out, "op1")
				assert.Equal(t, []string{"GET /v1/specifications", "POST /v1/clusters"}, f.requests())
				assert.Contains(t, f.body("POST /v1/clusters"), `"subnet_ids":`)
				assert.NotContains(t, f.body("POST /v1/clusters"), `"subnet_id":`)
			}

			if tc.lookup == "failed" {
				assert.Contains(t, stderr, "warning: cannot fetch specifications")
			} else {
				assert.Empty(t, stderr)
			}
		})
	}
}

func TestUpdateDeploymentRules(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"STANDARD", "BUSINESS"} {
		for _, instances := range []int{0, 1, 2, 3, 4} {
			for _, subnets := range []int{0, 1, 2, 3} {
				t.Run(fmt.Sprintf("%s/%d-instances/%d-subnets", mode, instances, subnets), func(t *testing.T) {
					t.Parallel()

					f := newFake(map[string]string{
						"GET /v1/clusters/c1":    `{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["old"]}`,
						"GET /v1/specifications": fmt.Sprintf(`{"specifications":[{"id":"s1","deployment_mode":%q,"max_hosts":%d}]}`, mode, map[string]int{"STANDARD": 2, "BUSINESS": 3}[mode]),
						"PATCH /v1/clusters/c1":  `{"id":"op1"}`,
					})
					_, err := run(t, f, false, "update", "c1", "--instances", strconv.Itoa(instances), "--subnet-id", strings.Join([]string{"sn1", "sn2", "sn3"}[:subnets], ","))

					allowed := instances >= 1 && instances <= 3 && subnets >= 1 && (subnets == 1 || subnets == instances) && (mode != "STANDARD" || (instances <= 2 && subnets == 1))
					if allowed {
						require.NoError(t, err)
						assert.Contains(t, f.requests(), "PATCH /v1/clusters/c1")
						assert.Contains(t, f.body("PATCH /v1/clusters/c1"), `"subnet_ids":{"value":`)
						assert.NotContains(t, f.body("PATCH /v1/clusters/c1"), `"subnet_id":`)
					} else {
						require.Error(t, err)
						assert.NotContains(t, f.requests(), "PATCH /v1/clusters/c1")
					}
				})
			}
		}
	}
}

func TestUpdateSpecificationChange(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, mode, body string
		flags            []string
		allowed          bool
	}{
		{"standard flag", "STANDARD", "", []string{"--spec", "s2"}, false},
		{"standard body", "STANDARD", `{"specification_id":"s2"}`, nil, false},
		{"standard unchanged", "STANDARD", "", []string{"--spec", "s1"}, true},
		{"business change", "BUSINESS", "", []string{"--spec", "s2"}, true},
		{"business missing", "BUSINESS", "", []string{"--spec", "missing"}, false},
		{"body restores original", "STANDARD", `{"specification_id":"s1"}`, []string{"--spec", "s2"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{
				"GET /v1/clusters/c1":    `{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"]}`,
				"GET /v1/specifications": fmt.Sprintf(`{"specifications":[{"id":"s1","deployment_mode":%q,"max_hosts":3},{"id":"s2","deployment_mode":"BUSINESS","max_hosts":3}]}`, tc.mode),
				"PATCH /v1/clusters/c1":  `{"id":"op1"}`,
			})

			args := append([]string{"update", "c1"}, tc.flags...)
			if tc.body != "" {
				args = append(args, "--body", tc.body)
			}

			out, err := run(t, f, false, args...)
			assert.Equal(t, []string{"GET /v1/clusters/c1", "GET /v1/specifications"}, f.requests()[:2])
			assert.Len(t, f.requests(), map[bool]int{true: 3, false: 2}[tc.allowed])

			if tc.allowed {
				require.NoError(t, err)
				assert.Contains(t, out, "op1")
				assert.Contains(t, f.requests(), "PATCH /v1/clusters/c1")
			} else {
				require.Error(t, err)
				assert.Empty(t, out)
				assert.NotContains(t, f.requests(), "PATCH /v1/clusters/c1")

				if tc.mode == "STANDARD" {
					assert.Contains(t, err.Error(), "standard: cluster resources cannot be changed")
				}
			}
		})
	}
}

func TestCreateBodyOverridesAndMaxHosts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body, want string
		max              int
	}{
		{"legacy subnet", `{"subnet_id":"legacy"}`, "subnet_id is deprecated", 2},
		{"body instances", `{"instances":3}`, "standard: maximum instances: 2", 2},
		{"body subnets", `{"subnet_ids":["a","b"]}`, "only available in business mode", 2},
		{"body empty subnets", `{"subnet_ids":[]}`, "at least one subnet", 2},
		{"body spec", `{"specification_id":"missing"}`, "no such specification: missing", 2},
		{"max hosts", `{"instances":2}`, "standard: maximum instances: 1", 1},
		{"bad body", `[]`, "--body", 2},
		{"bad instance type", `{"instances":"3"}`, "--body", 2},
		{"body valid", `{"instances":2,"subnet_ids":["sn2"],"future":true}`, "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{
				"GET /v1/specifications": fmt.Sprintf(`{"specifications":[{"id":"s1","deployment_mode":"STANDARD","max_hosts":%d}]}`, tc.max),
				"POST /v1/clusters":      `{"id":"op1"}`,
			})

			out, err := run(t, f, false, "create", "--name", "n", "--version", "17", "--spec", "s1", "--initial-database", "app", "--storage-gib", "50", "--subnet-id", "sn1", "--body", tc.body)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.NotContains(t, f.requests(), "POST /v1/clusters")
			} else {
				require.NoError(t, err)
				assert.Contains(t, out, "op1")
				assert.Contains(t, f.body("POST /v1/clusters"), `"future":true`)
			}
		})
	}
}

// Режима нет в схеме Cluster: human-вывод должен брать его из спецификации,
// по одному справочнику на версию, а --json — вообще не запрашивать справочник.
func TestClusterDeploymentDisplay(t *testing.T) {
	t.Parallel()

	single := `{"id":"c1","name":"pg","version":"17","specification_id":"s1","instances":2,"subnet_ids":["sn1","sn2"],"availability_zone_ids":["az1","az2"],"storage":{"pg_data_gb":50,"pg_wal_gb":20},"future":12345678901234567890}`
	for _, verb := range []string{"get", "list"} {
		for _, jsonOut := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", verb, jsonOut), func(t *testing.T) {
				t.Parallel()

				raw := single
				route := "GET /v1/clusters/c1"

				args := []string{"get", "c1"}
				if verb == "list" {
					raw = `{"clusters":[` + single + `,` + single + `]}`
					route = "GET /v1/clusters"
					args = []string{"list"}
				}

				f := newFake(map[string]string{route: raw, "GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"BUSINESS","max_hosts":3}]}`})
				out, err := run(t, f, jsonOut, args...)
				require.NoError(t, err)

				if jsonOut {
					assert.JSONEq(t, raw, out)
					assert.Equal(t, []string{route}, f.requests())
				} else {
					for _, value := range []string{"BUSINESS", "2", "sn1", "sn2", "az1", "az2"} {
						assert.Contains(t, out, value)
					}

					assert.Equal(t, []string{route, "GET /v1/specifications"}, f.requests())
				}
			})
		}
	}
}

// --wait у нового продукта должен получить и вывести созданный кластер,
// а не принять operation за Cluster или напечатать пустую таблицу.
func TestCreateWaitFetchesPostgreSQLCluster(t *testing.T) {
	t.Parallel()

	for _, jsonOut := range []bool{false, true} {
		f := newFake(map[string]string{
			"GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"STANDARD","max_hosts":2}]}`,
			"POST /v1/clusters":      `{"id":"op1","done":false,"resource_id":"c1"}`,
			"GET /v1/operations/op1": `{"id":"op1","done":true,"resource_id":"c1"}`,
			"GET /v1/clusters/c1":    `{"id":"c1","name":"pg","version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"]}`,
		})
		out, err := run(t, f, jsonOut, "create", "--name", "n", "--version", "17", "--spec", "s1", "--initial-database", "app", "--storage-gib", "50", "--subnet-id", "sn1", "--wait")
		require.NoError(t, err)
		assert.Contains(t, out, "c1")
		assert.Contains(t, out, "pg")
		assert.Contains(t, f.requests(), "GET /v1/operations/op1")
		assert.Contains(t, f.requests(), "GET /v1/clusters/c1")

		if !jsonOut {
			assert.Contains(t, out, "STANDARD")
		}
	}
}

// Клиентский отказ и битые ответы не должны превращаться в пустой успешный
// список. Типовые golden получают корректные схемы и этих краёв не содержат.
func TestClusterReadFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		args   []string
		routes map[string]string
		want   string
		setup  func(*state.State, *cobra.Command)
	}{
		{"list project", []string{"list"}, nil, "project", func(s *state.State, _ *cobra.Command) { s.Project = "" }},
		{"create project", []string{"create", "--name", "n", "--version", "17", "--spec", "s1", "--initial-database", "app", "--storage-gib", "50", "--subnet-id", "sn1"}, nil, "project", func(s *state.State, _ *cobra.Command) { s.Project = "" }},
		{"list client", []string{"list"}, nil, "client unavailable", func(s *state.State, _ *cobra.Command) {
			s.PostgreSQL = func() (*cloud.PostgreSQL, error) { return nil, errors.New("client unavailable") }
		}},
		{"get client", []string{"get", "c1"}, nil, "client unavailable", func(s *state.State, _ *cobra.Command) {
			s.PostgreSQL = func() (*cloud.PostgreSQL, error) { return nil, errors.New("client unavailable") }
		}},
		{"locales API", []string{"locales", "c1"}, nil, "API 404", nil},
		{"storage info API", []string{"storage-info", "c1"}, nil, "API 404", nil},
		{"list API", []string{"list"}, nil, "API 404", nil},
		{"get API", []string{"get", "c1"}, nil, "API 404", nil},
		{"list schema", []string{"list"}, map[string]string{"GET /v1/clusters": `{"clusters":[{"instances":"bad"}]}`}, "parse response", nil},
		{"get schema", []string{"get", "c1"}, map[string]string{"GET /v1/clusters/c1": `{"instances":"bad"}`}, "parse response", nil},
		{"get specs API", []string{"get", "c1"}, map[string]string{"GET /v1/clusters/c1": `{"id":"c1","version":"17"}`}, "", nil},
		{"get specs schema", []string{"get", "c1"}, map[string]string{"GET /v1/clusters/c1": `{"id":"c1"}`, "GET /v1/specifications": `{"specifications":true}`}, "", nil},
		{"list specs API", []string{"list"}, map[string]string{"GET /v1/clusters": `{"clusters":[{"id":"c1","version":"17"},{"id":"c2","version":"17"}]}`}, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(tc.routes)

			out, stderr, err := runWith(t, f, false, tc.setup, tc.args...)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.Empty(t, out)
			} else {
				require.NoError(t, err)
				assert.Contains(t, out, "c1")
				assert.Contains(t, stderr, "warning: cannot fetch specifications")
				assert.NotContains(t, out, "BUSINESS")
				assert.NotContains(t, out, "STANDARD")

				if tc.args[0] == "get" {
					assert.Contains(t, out, "deployment_mode:")

					for _, line := range strings.Split(out, "\n") {
						if strings.HasPrefix(line, "deployment_mode:") {
							assert.Equal(t, "deployment_mode:", strings.TrimSpace(line))
						}
					}
				} else {
					assert.Contains(t, out, "MODE")
					assert.Contains(t, out, "c2")
				}

				assert.Equal(t, []string{"GET /v1/clusters" + map[string]string{"get": "/c1", "list": ""}[tc.args[0]], "GET /v1/specifications"}, f.requests())
			}
		})
	}
}

func TestClusterWriteFailures(t *testing.T) {
	t.Parallel()

	create := []string{"create", "--name", "n", "--version", "17", "--spec", "s1", "--initial-database", "app", "--storage-gib", "50", "--subnet-id", "sn1"}
	for _, tc := range []struct {
		name   string
		args   []string
		routes map[string]string
		want   string
		setup  func(*state.State, *cobra.Command)
	}{
		{"create logging", append(append([]string{}, create...), "--log-group-id", "lg"), nil, "requires --logging", nil},
		{"create file", append(append([]string{}, create...), "--body", "@/missing-pg-body"), nil, "--body", nil},
		{"create client", create, nil, "client unavailable", func(s *state.State, _ *cobra.Command) {
			s.PostgreSQL = func() (*cloud.PostgreSQL, error) { return nil, errors.New("client unavailable") }
		}},
		{"create specs schema", create, map[string]string{"GET /v1/specifications": `{"specifications":true}`, "POST /v1/clusters": `{"id":"op1"}`}, "", nil},
		{"create API", create, map[string]string{"GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"STANDARD","max_hosts":2}]}`}, "API 404", nil},
		{"update unchanged", []string{"update", "c1"}, nil, "specify", nil},
		{"update file", []string{"update", "c1", "--body", "@/missing-pg-body"}, nil, "--body", nil},
		{"update client", []string{"update", "c1", "--instances", "2"}, nil, "client unavailable", func(s *state.State, _ *cobra.Command) {
			s.PostgreSQL = func() (*cloud.PostgreSQL, error) { return nil, errors.New("client unavailable") }
		}},
		{"update cluster API", []string{"update", "c1", "--instances", "2"}, nil, "API 404", nil},
		{"update cluster schema", []string{"update", "c1", "--instances", "2"}, map[string]string{"GET /v1/clusters/c1": `{"instances":"bad"}`}, "parse response", nil},
		{"update specs API", []string{"update", "c1", "--instances", "2"}, map[string]string{"GET /v1/clusters/c1": `{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"]}`, "PATCH /v1/clusters/c1": `{"id":"op1"}`}, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(tc.routes)

			out, stderr, err := runWith(t, f, false, tc.setup, tc.args...)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.Empty(t, out)
			} else {
				require.NoError(t, err)
				assert.Contains(t, out, "op1")
				assert.Contains(t, stderr, "warning")
			}
		})
	}
}

// Находка ревью на HTTP-фейке: WAL-only отправлял pg_data_gb=0;
// полные запросы coverageRuns и golden всегда задают оба диска.
func TestUpdateWALKeepsDataDisk(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{
		"GET /v1/clusters/c1":    `{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"],"storage":{"pg_data_gb":100}}`,
		"GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"BUSINESS","max_hosts":3}]}`,
		"PATCH /v1/clusters/c1":  `{"id":"op1"}`,
	})
	_, err := run(t, f, false, "update", "c1", "--wal-gib", "20")
	require.NoError(t, err)
	assert.JSONEq(t, `{"cluster_id":"c1","storage":{"pg_data_gb":100,"pg_wal_gb":20}}`, f.body("PATCH /v1/clusters/c1"))
}

// PATCH использует объект value для подсетей; --body обязан проверяться
// после замены полей, а не только по флагам из матрицы.
func TestUpdateBodyRules(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body, want string }{
		{"legacy", `{"subnet_id":"legacy"}`, "subnet_id is deprecated"},
		{"instances", `{"instances":0}`, "at least 1 instance"},
		{"empty subnets", `{"subnet_ids":{"value":[]}}`, "at least one subnet"},
		{"subnet count", `{"instances":2,"subnet_ids":{"value":["a","b","c"]}}`, "counts must match"},
		{"wrong schema", `{"subnet_ids":["a","b"]}`, "--body"},
		{"invalid JSON", `broken`, "--body"},
		{"valid override", `{"instances":2,"subnet_ids":{"value":["a","b"]}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{
				"GET /v1/clusters/c1":    `{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"]}`,
				"GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"BUSINESS","max_hosts":3}]}`,
				"PATCH /v1/clusters/c1":  `{"id":"op1"}`,
			})

			out, err := run(t, f, false, "update", "c1", "--instances", "3", "--body", tc.body)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.NotContains(t, f.requests(), "PATCH /v1/clusters/c1")
			} else {
				require.NoError(t, err)
				assert.Contains(t, out, "op1")
				assert.JSONEq(t, `{"cluster_id":"c1","instances":2,"subnet_ids":{"value":["a","b"]}}`, f.body("PATCH /v1/clusters/c1"))
			}
		})
	}
}

// Data-only не должен терять уже существующий WAL-диск. Общие HTTP-контракты
// проверяют полностью заданный storage и не знают состояние из GET кластера.
func TestUpdateDataKeepsWALDisk(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		wal   int
		flags []string
		want  string
	}{
		{"data only", 20, []string{"--storage-gib", "200"}, `{"cluster_id":"c1","storage":{"pg_data_gb":200,"pg_wal_gb":20}}`},
		{"explicit WAL", 20, []string{"--storage-gib", "200", "--wal-gib", "40"}, `{"cluster_id":"c1","storage":{"pg_data_gb":200,"pg_wal_gb":40}}`},
		{"no WAL", 0, []string{"--storage-gib", "200"}, `{"cluster_id":"c1","storage":{"pg_data_gb":200}}`},
		{"spec only", 20, []string{"--spec", "s1"}, `{"cluster_id":"c1","specification_id":"s1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{
				"GET /v1/clusters/c1":    fmt.Sprintf(`{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"],"storage":{"pg_data_gb":100,"pg_wal_gb":%d}}`, tc.wal),
				"GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"BUSINESS","max_hosts":3}]}`,
				"PATCH /v1/clusters/c1":  `{"id":"op1"}`,
			})
			out, err := run(t, f, false, append([]string{"update", "c1"}, tc.flags...)...)
			require.NoError(t, err)
			assert.Contains(t, out, "op1")
			assert.JSONEq(t, tc.want, f.body("PATCH /v1/clusters/c1"))
		})
	}
}

// Недоступность каталога или исчезнувшая текущая спецификация не должны
// запрещать PATCH; обычные golden всегда содержат текущую спецификацию.
func TestUpdateWithoutSpecification(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, specs, warning string }{
		{"request failed", "", "API 404"},
		{"current missing", `{"specifications":[{"id":"s2","deployment_mode":"STANDARD","max_hosts":2}]}`, "no such specification: s1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			routes := map[string]string{
				"GET /v1/clusters/c1":   `{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"]}`,
				"PATCH /v1/clusters/c1": `{"id":"op1"}`,
			}
			if tc.specs != "" {
				routes["GET /v1/specifications"] = tc.specs
			}

			f := newFake(routes)
			out, stderr, err := runWith(t, f, false, nil, "update", "c1", "--spec", "s2", "--instances", "3", "--subnet-id", "a,b,c")
			require.NoError(t, err)
			assert.Contains(t, out, "op1")
			assert.Contains(t, stderr, "warning: cannot fetch specifications")
			assert.Contains(t, stderr, tc.warning)
			assert.Equal(t, []string{"GET /v1/clusters/c1", "GET /v1/specifications", "PATCH /v1/clusters/c1"}, f.requests())
			assert.JSONEq(t, `{"cluster_id":"c1","specification_id":"s2","instances":3,"subnet_ids":{"value":["a","b","c"]}}`, f.body("PATCH /v1/clusters/c1"))
		})
	}
}

// Неизвестный режим не отменяет общие ограничения топологии. Проверяем
// create и update: их разные источники текущей топологии не покрыты одним --help.
func TestUnknownDeploymentRules(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"", "DEPLOYMENT_MODE_UNSPECIFIED", "FUTURE"} {
		for _, verb := range []string{"create", "update"} {
			for _, tc := range []struct {
				name          string
				instances     int
				subnets, want string
			}{
				{"zero", 0, "a", "at least 1 instance"},
				{"four", 4, "a", "business: maximum instances: 3"},
				{"mismatch", 2, "a,b,c", "counts must match"},
				{"empty subnets", 1, "", "at least one subnet"},
				{"single zone", 3, "a", ""},
				{"multi zone", 3, "a,b,c", ""},
			} {
				t.Run(mode+"/"+verb+"/"+tc.name, func(t *testing.T) {
					t.Parallel()

					write := "POST /v1/clusters"

					args := []string{"create", "--name", "n", "--version", "17", "--spec", "s1", "--initial-database", "app", "--storage-gib", "50"}
					if verb == "update" {
						write = "PATCH /v1/clusters/c1"
						args = []string{"update", "c1"}
					}

					args = append(args, "--instances", strconv.Itoa(tc.instances), "--subnet-id", tc.subnets)
					f := newFake(map[string]string{
						"GET /v1/clusters/c1":    `{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"]}`,
						"GET /v1/specifications": fmt.Sprintf(`{"specifications":[{"id":"s1","deployment_mode":%q}]}`, mode),
						write:                    `{"id":"op1"}`,
					})

					out, err := run(t, f, false, args...)
					if tc.want != "" {
						require.ErrorContains(t, err, tc.want)
						assert.Empty(t, out)
						assert.NotContains(t, f.requests(), write)
					} else {
						require.NoError(t, err)
						assert.Contains(t, out, "op1")
						assert.Contains(t, f.requests(), write)
					}
				})
			}
		}
	}
}

func TestClusterWALDisplay(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		wal     int
		jsonOut bool
	}{
		{0, false}, {0, true}, {20, false},
	} {
		t.Run(fmt.Sprintf("wal-%d/json-%t", tc.wal, tc.jsonOut), func(t *testing.T) {
			t.Parallel()

			raw := fmt.Sprintf(`{"id":"c1","deployment_mode":"BUSINESS","storage":{"pg_data_gb":100,"pg_wal_gb":%d}}`, tc.wal)
			f := newFake(map[string]string{"GET /v1/clusters/c1": raw})
			out, err := run(t, f, tc.jsonOut, "get", "c1")
			require.NoError(t, err)

			if tc.jsonOut {
				assert.JSONEq(t, raw, out)
			} else if tc.wal > 0 {
				assert.Contains(t, out, "pg_wal_gb:")
				assert.Contains(t, out, "20")
			} else {
				assert.Contains(t, out, "pg_data_gb:")
				assert.NotContains(t, out, "pg_wal_gb:")
			}

			assert.Equal(t, []string{"GET /v1/clusters/c1"}, f.requests())
		})
	}
}

// Типовые golden не передают лишних аргументов: опечатка после ID должна
// завершаться до запроса, даже когда --yes задан.
func TestClusterCommandRejectsExtraArgument(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		verb  string
		flags []string
	}{
		{"suspend", []string{"--yes"}},
		{"resume", []string{"--yes"}},
		{"switchover", []string{"--yes"}},
		{verb: "storage-info"},
		{verb: "locales"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			out, err := run(t, f, true, append([]string{tc.verb, "c1", "extra"}, tc.flags...)...)
			require.ErrorContains(t, err, "accepts 1 arg(s), received 2")
			assert.Equal(t, 1, output.ExitCode(err))
			assert.Empty(t, out)
			assert.Empty(t, f.requests())
		})
	}
}
