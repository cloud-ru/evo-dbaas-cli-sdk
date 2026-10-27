package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

const (
	testProject = "11111111-1111-4111-8111-111111111111"
	testSubnet  = "22222222-2222-4222-8222-222222222222"
	testZone    = "33333333-3333-4333-8333-333333333333"
	testVPC     = "44444444-4444-4444-8444-444444444444"
	subnetPath  = "/api/v1/subnets/" + testSubnet
	subnetJSON  = `{"id":"22222222-2222-4222-8222-222222222222","name":"private","subnet_address":"10.0.9.0/24","availability_zone":{"name":"ru.AZ-1"},"vpc_id":"44444444-4444-4444-8444-444444444444","state":"created","future":17}`
)

type reply struct {
	body   string
	status int
}
type request struct {
	key, body string
	query     url.Values
}
type fake struct {
	mu     sync.Mutex
	routes map[string][]reply
	calls  []request
}

func newFake(routes map[string][]reply) *fake { return &fake{routes: routes} }

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.calls = append(f.calls, request{key: key, body: string(body), query: r.URL.Query()})
	responses := f.routes[key]

	response := reply{body: `{"message":"no route"}`, status: http.StatusNotFound}
	if len(responses) > 0 {
		response = responses[0]
		if len(responses) > 1 {
			f.routes[key] = responses[1:]
		}
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if response.status != 0 {
		w.WriteHeader(response.status)
	}

	_, _ = io.WriteString(w, response.body)
}

func (f *fake) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]request(nil), f.calls...)
}

func run(t *testing.T, f *fake, json bool, setup func(*state.State), args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	opts := cloud.Options{HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil }}
	c, err := cloud.NewCompute(srv.URL, opts)
	require.NoError(t, err)
	v, err := cloud.NewVPC(srv.URL, opts)
	require.NoError(t, err)

	var out, stderr bytes.Buffer

	s := &state.State{Project: testProject, Printer: &output.Printer{Out: &out, Err: &stderr, JSON: json}, Timeout: time.Second, PollDelay: time.Millisecond, Compute: func() (*cloud.Compute, error) { return c, nil }, VPC: func() (*cloud.VPC, error) { return v, nil }}
	if setup != nil {
		setup(s)
	}

	cmd := NewCmd(s)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(t.Context())

	return out.String(), stderr.String(), err
}

func TestSubnetListFiltersAndOutput(t *testing.T) {
	t.Parallel()

	for _, zone := range []string{"", testZone, "ru.AZ-1"} {
		for _, json := range []bool{false, true} {
			t.Run(zone+map[bool]string{false: "/table", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()

				f := newFake(map[string][]reply{
					"GET /api/v1/availability-zones": {{body: `[{"id":"33333333-3333-4333-8333-333333333333","name":"ru.AZ-1"}]`}},
					"GET /api/v1/subnets":            {{body: `{"items":[` + subnetJSON + `],"total":1}`}},
				})

				args := []string{"subnets", "list", "--vpc-id", testVPC, "--name", "private + db"}
				if zone != "" {
					args = append(args, "--zone", zone)
				}

				out, _, err := run(t, f, json, nil, args...)
				require.NoError(t, err)

				if json {
					assert.JSONEq(t, `{"items":[`+subnetJSON+`],"total":1}`, out)
				} else {
					assert.Equal(t, []string{"ID", "NAME", "CIDR", "ZONE", "STATE", testSubnet, "private", "10.0.9.0/24", "ru.AZ-1", "created"}, strings.Fields(out))
				}

				calls := f.requests()

				count := 1
				if zone == "ru.AZ-1" {
					count++
					require.Len(t, calls, count)
					assert.Equal(t, "GET /api/v1/availability-zones", calls[0].key)
					assert.Empty(t, calls[0].query)
				} else {
					require.Len(t, calls, count)
				}

				last := calls[len(calls)-1]
				assert.Equal(t, "GET /api/v1/subnets", last.key)

				query := url.Values{"project_id": {testProject}, "vpc_id": {testVPC}, "name": {"private + db"}, "offset": {"0"}, "limit": {"100"}}
				if zone != "" {
					query["availability_zone_ids"] = []string{testZone}
				}

				assert.Equal(t, query, last.query)
				assert.Empty(t, last.body)
			})
		}
	}
}

func TestSubnetGet(t *testing.T) {
	t.Parallel()

	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "table", true: "json"}[json], func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string][]reply{"GET " + subnetPath: {{body: subnetJSON}}})
			out, _, err := run(t, f, json, nil, "subnets", "get", testSubnet)
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, subnetJSON, out)
			} else {
				assert.Equal(t, []string{"FIELD", "VALUE", "id", testSubnet, "name", "private", "cidr", "10.0.9.0/24", "zone", "ru.AZ-1", "vpc_id", testVPC, "state", "created"}, strings.Fields(out))
			}

			assert.Equal(t, []request{{key: "GET " + subnetPath, query: url.Values{}}}, f.requests())
		})
	}
}

func TestSubnetCreate(t *testing.T) {
	t.Parallel()

	for _, wait := range []bool{false, true} {
		for _, json := range []bool{false, true} {
			t.Run(map[bool]string{false: "immediate", true: "wait"}[wait]+map[bool]string{false: "/table", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()

				initial := subnetJSON
				if wait {
					initial = `{"id":"22222222-2222-4222-8222-222222222222","state":"creating"}`
				}

				f := newFake(map[string][]reply{"POST /api/v1/subnets": {{body: initial}}, "GET " + subnetPath: {{body: `{"state":"creating"}`}, {body: subnetJSON}}})

				args := []string{"subnets", "create", "--name", "private", "--zone", testZone, "--cidr", "10.0.9.0/24", "--vpc-id", testVPC, "--description", "private db", "--gateway", "10.0.9.1", "--dns", "1.1.1.1", "--dns", "8.8.8.8", "--isolated"}
				if wait {
					args = append(args, "--wait")
				}

				out, _, err := run(t, f, json, nil, args...)
				require.NoError(t, err)

				if json {
					assert.JSONEq(t, subnetJSON, out)
				} else {
					assert.Contains(t, out, "private")
					assert.Contains(t, out, "created")
				}

				calls := f.requests()
				if wait {
					require.Len(t, calls, 3)
					assert.Equal(t, "GET "+subnetPath, calls[1].key)
					assert.Equal(t, "GET "+subnetPath, calls[2].key)
				} else {
					require.Len(t, calls, 1)
				}

				assert.Equal(t, "POST /api/v1/subnets", calls[0].key)
				assert.Empty(t, calls[0].query)
				assert.JSONEq(t, `{"project_id":"11111111-1111-4111-8111-111111111111","name":"private","availability_zone_id":"33333333-3333-4333-8333-333333333333","subnet_address":"10.0.9.0/24","vpc_id":"44444444-4444-4444-8444-444444444444","description":"private db","default_gateway":"10.0.9.1","dns_servers":["1.1.1.1","8.8.8.8"],"routed_network":false}`, calls[0].body)
			})
		}
	}
}

func TestSubnetWaitFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		responses []reply
		want      string
		code      int
	}{
		{"failed state", []reply{{body: `{"state":"error"}`}}, "is in state error", 1},
		{"poll refused", []reply{{status: http.StatusBadRequest, body: `{"message":"bad poll"}`}}, "bad poll", 8},
		{"transient then created", []reply{{status: http.StatusServiceUnavailable, body: `{"message":"retry"}`}, {body: subnetJSON}}, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string][]reply{"POST /api/v1/subnets": {{body: `{"id":"22222222-2222-4222-8222-222222222222"}`}}, "GET " + subnetPath: tc.responses})
			out, _, err := run(t, f, true, nil, "subnets", "create", "--name", "private", "--zone", testZone, "--cidr", "10.0.9.0/24", "--wait")
			assert.Equal(t, tc.code, output.ExitCode(err))

			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.Empty(t, out)
			} else {
				require.NoError(t, err)
				assert.JSONEq(t, subnetJSON, out)
			}

			if tc.code == 1 {
				var ee *base.ExitError
				require.ErrorAs(t, err, &ee)
				assert.Equal(t, 1, ee.Code)
			}
		})
	}
}

func TestSubnetDelete(t *testing.T) {
	t.Parallel()

	for _, wait := range []bool{false, true} {
		for _, json := range []bool{false, true} {
			t.Run(map[bool]string{false: "immediate", true: "wait"}[wait]+map[bool]string{false: "/human", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()

				f := newFake(map[string][]reply{"DELETE " + subnetPath: {{status: http.StatusNoContent}}, "GET " + subnetPath: {{body: `{"state":"deleting"}`}, {status: http.StatusNotFound, body: `{"message":"not found"}`}}})

				args := []string{"subnets", "delete", testSubnet, "--yes"}
				if wait {
					args = append(args, "--wait")
				}

				out, stderr, err := run(t, f, json, nil, args...)
				require.NoError(t, err)

				if json {
					assert.JSONEq(t, `{"deleted":"22222222-2222-4222-8222-222222222222"}`, out)
					assert.Empty(t, stderr)
				} else {
					assert.Empty(t, out)
					assert.Contains(t, stderr, "deleted "+testSubnet)
				}

				calls := f.requests()

				want := []request{{key: "DELETE " + subnetPath, query: url.Values{}}}
				if wait {
					want = append(want, request{key: "GET " + subnetPath, query: url.Values{}}, request{key: "GET " + subnetPath, query: url.Values{}})
				}

				assert.Equal(t, want, calls)
			})
		}
	}
}

func TestNetworkRefusesBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                 string
		args                 []string
		noProject, exitError bool
		want                 string
	}{
		{name: "subnets project", args: []string{"subnets", "list"}, noProject: true, want: "project not set"},
		{name: "subnet create project", args: []string{"subnets", "create", "--name", "n", "--zone", testZone, "--cidr", "10.0.0.0/24"}, noProject: true, want: "project not set"},
		{name: "subnet flags", args: []string{"subnets", "create"}, want: `required flag(s) "cidr", "name", "zone" not set`},
		{name: "subnet delete", args: []string{"subnets", "delete", testSubnet}, want: "rerun with --yes", exitError: true},
		{name: "vpc delete", args: []string{"vpcs", "delete", "v1"}, want: "rerun with --yes", exitError: true},
		{name: "vpc flags", args: []string{"vpcs", "create"}, want: `required flag(s) "name" not set`},
		{name: "vpc list project", args: []string{"vpcs", "list"}, noProject: true, want: "project not set"},
		{name: "vpc create project", args: []string{"vpcs", "create", "--name", "n"}, noProject: true, want: "project not set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			out, _, err := run(t, f, false, func(s *state.State) {
				if tc.noProject {
					s.Project = ""
				}
			}, tc.args...)
			require.ErrorContains(t, err, tc.want)
			assert.Equal(t, 1, output.ExitCode(err))

			if tc.exitError {
				var ee *base.ExitError
				require.ErrorAs(t, err, &ee)
				assert.Equal(t, 1, ee.Code)
			}

			if tc.noProject {
				require.ErrorIs(t, err, state.ErrNoProject)
			}

			assert.Empty(t, out)
			assert.Empty(t, f.requests())
		})
	}
}

func TestNetworkClientAndAPIErrors(t *testing.T) {
	t.Parallel()

	clientErr := errors.New("client unavailable")
	for _, args := range [][]string{
		{"subnets", "list"},
		{"subnets", "get", testSubnet},
		{"subnets", "create", "--name", "n", "--zone", testZone, "--cidr", "10.0.0.0/24"},
		{"subnets", "delete", testSubnet, "--yes"},
		{"vpcs", "list"},
		{"vpcs", "get", "v1"},
		{"vpcs", "create", "--name", "n"},
		{"vpcs", "delete", "v1", "--yes"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			for _, clientFailure := range []bool{false, true} {
				f := newFake(nil)

				out, _, err := run(t, f, false, func(s *state.State) {
					if clientFailure {
						s.Compute = func() (*cloud.Compute, error) { return nil, clientErr }
						s.VPC = func() (*cloud.VPC, error) { return nil, clientErr }
					}
				}, args...)
				if clientFailure {
					require.ErrorIs(t, err, clientErr)
					assert.Empty(t, f.requests())
				} else {
					require.ErrorContains(t, err, "API 404: no route")
					assert.Len(t, f.requests(), 1)
				}

				assert.Equal(t, 1, output.ExitCode(err))
				assert.Empty(t, out)
			}
		})
	}
}

func TestSubnetZoneLookupErrorsStopMutationOrList(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"subnets", "list", "--zone", "ru.AZ-9"},
		{"subnets", "create", "--name", "private", "--zone", "ru.AZ-9", "--cidr", "10.0.0.0/24"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string][]reply{"GET /api/v1/availability-zones": {{body: `[{"id":"33333333-3333-4333-8333-333333333333","name":"ru.AZ-1"}]`}}})
			out, _, err := run(t, f, false, nil, args...)
			require.ErrorContains(t, err, "not found; see zones list")
			assert.Equal(t, 1, output.ExitCode(err))
			assert.Empty(t, out)
			assert.Equal(t, []request{{key: "GET /api/v1/availability-zones", query: url.Values{}}}, f.requests())
		})
	}
}

func TestSubnetDeleteWaitRejectsPermanentPollingFailure(t *testing.T) {
	t.Parallel()

	f := newFake(map[string][]reply{
		"DELETE " + subnetPath: {{status: http.StatusNoContent}},
		"GET " + subnetPath:    {{status: http.StatusBadRequest, body: `{"message":"cannot poll"}`}},
	})
	out, stderr, err := run(t, f, true, nil, "subnets", "delete", testSubnet, "--yes", "--wait")
	require.ErrorContains(t, err, "cannot poll")
	assert.Equal(t, 8, output.ExitCode(err))
	assert.Empty(t, out)
	assert.Empty(t, stderr)
	assert.Equal(t, []request{{key: "DELETE " + subnetPath, query: url.Values{}}, {key: "GET " + subnetPath, query: url.Values{}}}, f.requests())
}

func TestVPCWaitPropagatesOperationClientError(t *testing.T) {
	t.Parallel()

	clientErr := errors.New("poll client unavailable")
	f := newFake(map[string][]reply{"POST /v1/vpcs": {{body: `{"id":"op1"}`}}})
	clients := 0
	out, _, err := run(t, f, true, func(s *state.State) {
		factory := s.VPC
		s.VPC = func() (*cloud.VPC, error) {
			clients++
			if clients == 2 {
				return nil, clientErr
			}

			return factory()
		}
	}, "vpcs", "create", "--name", "private", "--wait")
	require.ErrorIs(t, err, clientErr)
	assert.Equal(t, 1, output.ExitCode(err))
	assert.Equal(t, 2, clients)
	assert.Empty(t, out)

	calls := f.requests()
	require.Len(t, calls, 1)
	assert.Equal(t, "POST /v1/vpcs", calls[0].key)
	assert.Empty(t, calls[0].query)
	assert.JSONEq(t, `{"name":"private","projectId":"11111111-1111-4111-8111-111111111111"}`, calls[0].body)
}
