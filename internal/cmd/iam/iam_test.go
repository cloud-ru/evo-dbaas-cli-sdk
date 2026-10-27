package iam

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
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
	routes map[string]reply
	calls  []request
	before http.HandlerFunc
}

func newFake(routes map[string]reply) *fake { return &fake{routes: routes} }

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.before != nil {
		f.before(w, r)
	}

	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.calls = append(f.calls, request{key: key, body: string(body), query: r.URL.Query()})
	response, ok := f.routes[key]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"no route"}`)

		return
	}

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
	c, err := cloud.NewIAM(srv.URL, cloud.Options{HTTPClient: srv.Client(), Auth: func(context.Context, *http.Request) error { return nil }})
	require.NoError(t, err)

	var out, stderr bytes.Buffer

	s := &state.State{Project: "P", Printer: &output.Printer{Out: &out, Err: &stderr, JSON: json}, IAM: func() (*cloud.IAM, error) { return c, nil }}
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

func TestServiceAccountListAliasesAndOutput(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"service_accounts", "serviceAccounts"} {
		for _, json := range []bool{false, true} {
			t.Run(key+map[bool]string{false: "/table", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()

				raw := `{"` + key + `":[{"id":"sa1","name":"robot","type":"regular","enabled":true,"future":17}]}`
				f := newFake(map[string]reply{"GET /api/v2/serviceAccounts": {body: raw}})
				out, stderr, err := run(t, f, json, nil, "sa", "list")
				require.NoError(t, err)

				if json {
					assert.JSONEq(t, raw, out)
				} else {
					assert.Equal(t, []string{"ID", "NAME", "TYPE", "ENABLED", "sa1", "robot", "regular", "true"}, strings.Fields(out))
				}

				assert.Empty(t, stderr)
				assert.Equal(t, []request{{key: "GET /api/v2/serviceAccounts", query: url.Values{"target.projectId": {"P"}, "pageSize": {"1000"}}}}, f.requests())
			})
		}
	}
}

func TestIAMGetPreservesJSONForHumansAndMachines(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, path, raw string
		args            []string
	}{
		{"account", "/api/v2/serviceAccounts/sa1", `{"id":"sa1","name":"robot","future":17}`, []string{"service-accounts", "get", "sa1"}},
		{"key", "/api/v2/serviceAccounts/sa1/accessKeys/ak1", `{"id":"ak1","key_id":"KID","future":17}`, []string{"access-keys", "get", "sa1", "ak1"}},
		{"role", "/api/v3/permissions/pm1", `{"id":"pm1","role_name":"vpc.admin","future":17}`, []string{"roles", "get", "pm1"}},
	} {
		for _, json := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/human", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()

				f := newFake(map[string]reply{"GET " + tc.path: {body: tc.raw}})
				out, stderr, err := run(t, f, json, nil, tc.args...)
				require.NoError(t, err)
				assert.JSONEq(t, tc.raw, out)
				assert.NotContains(t, out, "secret")
				assert.Empty(t, stderr)
				assert.Equal(t, []request{{key: "GET " + tc.path, query: url.Values{}}}, f.requests())
			})
		}
	}
}

func TestServiceAccountCreate(t *testing.T) {
	t.Parallel()

	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "human", true: "json"}[json], func(t *testing.T) {
			t.Parallel()

			const raw = `{"id":"sa1","name":"robot","future":17}`

			f := newFake(map[string]reply{"POST /api/v2/serviceAccounts": {body: raw}})
			out, stderr, err := run(t, f, json, nil, "service-accounts", "create", "--name", "robot", "--description", "database agent")
			require.NoError(t, err)

			if json {
				assert.JSONEq(t, raw, out)
			} else {
				assert.Equal(t, "created sa1 robot\n", out)
			}

			assert.Empty(t, stderr)

			calls := f.requests()
			require.Len(t, calls, 1)
			assert.Equal(t, "POST /api/v2/serviceAccounts", calls[0].key)
			assert.Empty(t, calls[0].query)
			assert.JSONEq(t, `{"name":"robot","description":"database agent","target":{"projectId":"P"}}`, calls[0].body)
		})
	}
}

func TestIAMConfirmedMutations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, path, message, refuse string
		args                        []string
	}{
		{"account", "/api/v2/serviceAccounts/sa1", "deleted sa1", "deletion cannot be undone: rerun with --yes", []string{"sa", "delete", "sa1"}},
		{"key", "/api/v2/serviceAccounts/sa1/accessKeys/ak1", "revoked ak1", "deletion cannot be undone: rerun with --yes", []string{"access-keys", "delete", "sa1", "ak1"}},
		{"role", "/api/v3/permissions/pm1", "revoked pm1", "revoking a role requires --yes", []string{"roles", "revoke", "pm1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			out, _, err := run(t, f, false, nil, tc.args...)
			require.EqualError(t, err, tc.refuse)

			var ee *base.ExitError
			require.ErrorAs(t, err, &ee)
			assert.Equal(t, 1, ee.Code)
			assert.Equal(t, 1, output.ExitCode(err))
			assert.Empty(t, f.requests())
			assert.Empty(t, out)

			for _, json := range []bool{false, true} {
				f := newFake(map[string]reply{"DELETE " + tc.path: {body: `{}`}})
				args := append(append([]string(nil), tc.args...), "--yes")
				out, stderr, err := run(t, f, json, nil, args...)
				require.NoError(t, err)

				if json {
					assert.JSONEq(t, `{}`, out)
					assert.Empty(t, stderr)
				} else {
					assert.Empty(t, out)
					assert.Equal(t, tc.message+"\n", stderr)
				}

				assert.Equal(t, []request{{key: "DELETE " + tc.path, query: url.Values{}}}, f.requests())
			}
		})
	}
}

func TestAccessKeyList(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ key, fields string }{
		{"access_keys", `"key_id":"KID","expired_at":"2027-01-01"`},
		{"accessKeys", `"keyId":"KID","expiredAt":"2027-01-01"`},
	} {
		for _, json := range []bool{false, true} {
			t.Run(tc.key+map[bool]string{false: "/table", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()

				raw := `{"` + tc.key + `":[{"id":"ak1",` + tc.fields + `,"description":"agent","future":17}]}`
				f := newFake(map[string]reply{"GET /api/v2/serviceAccounts/sa1/accessKeys": {body: raw}})
				out, stderr, err := run(t, f, json, nil, "access-keys", "list", "sa1")
				require.NoError(t, err)

				if json {
					assert.JSONEq(t, raw, out)
				} else {
					assert.Equal(t, []string{"ID", "KEY_ID", "EXPIRES", "DESCRIPTION", "ak1", "KID", "2027-01-01", "agent"}, strings.Fields(out))
				}

				assert.NotContains(t, out, "secret")
				assert.Empty(t, stderr)
				assert.Equal(t, []request{{key: "GET /api/v2/serviceAccounts/sa1/accessKeys", query: url.Values{}}}, f.requests())
			})
		}
	}
}

func TestAccessKeyCreateReservesPrivateFileAndHidesSecret(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"key_id", "keyId"} {
		for _, json := range []bool{false, true} {
			t.Run(key+map[bool]string{false: "/human", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()
				path := filepath.Join(t.TempDir(), "key.env")
				f := newFake(map[string]reply{"POST /api/v2/serviceAccounts/sa1/accessKeys": {body: `{"id":"ak1","` + key + `":"KID","secret":"super-secret","expiredAt":"2027-01-01"}`}})
				f.before = func(_ http.ResponseWriter, _ *http.Request) {
					info, err := os.Stat(path)
					assert.NoError(t, err)

					if info != nil {
						assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
						assert.Zero(t, info.Size())
					}
				}
				out, stderr, err := run(t, f, json, nil, "access-keys", "create", "sa1", "--env-file", path, "--description", "agent", "--ttl", "720h")
				require.NoError(t, err)
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, "CLOUDRU_KEY_ID=KID\nCLOUDRU_KEY_SECRET=super-secret\nCLOUDRU_PROJECT_ID=P\n", string(data))

				info, err := os.Stat(path)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
				assert.NotContains(t, out, "super-secret")
				assert.NotContains(t, stderr, "super-secret")

				if json {
					assert.JSONEq(t, `{"id":"ak1","key_id":"KID","expired_at":"2027-01-01","env_file":"`+path+`"}`, out)
					assert.NotContains(t, out, `"secret"`)
				} else {
					assert.Equal(t, "key KID saved to "+path+" (secret omitted from output)\n", out)
				}

				assert.Empty(t, stderr)

				calls := f.requests()
				require.Len(t, calls, 1)
				assert.Equal(t, "POST /api/v2/serviceAccounts/sa1/accessKeys", calls[0].key)
				assert.Empty(t, calls[0].query)
				assert.JSONEq(t, `{"description":"agent","ttl":"720h"}`, calls[0].body)
			})
		}
	}
}

func TestAccessKeyCreateRefusesOccupiedPathAndCleansUpFailures(t *testing.T) {
	t.Parallel()

	clientErr := errors.New("iam unavailable")
	for _, tc := range []struct {
		name, raw, want         string
		status                  int
		occupied, clientFailure bool
	}{
		{name: "occupied", occupied: true, want: "a new path is required"},
		{name: "API", status: http.StatusBadRequest, raw: `{"message":"bad request"}`, want: "API 400: bad request"},
		{name: "missing key id", raw: `{"secret":"super-secret"}`, want: "API did not return key_id and secret"},
		{name: "missing secret", raw: `{"key_id":"KID"}`, want: "API did not return key_id and secret"},
		{name: "malformed response", raw: `not JSON`, want: "API did not return key_id and secret"},
		{name: "client", clientFailure: true, want: "iam unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "key.env")
			if tc.occupied {
				require.NoError(t, os.WriteFile(path, []byte("existing"), 0o600))
			}

			f := newFake(map[string]reply{"POST /api/v2/serviceAccounts/sa1/accessKeys": {body: tc.raw, status: tc.status}})
			out, stderr, err := run(t, f, true, func(s *state.State) {
				if tc.clientFailure {
					s.IAM = func() (*cloud.IAM, error) { return nil, clientErr }
				}
			}, "access-keys", "create", "sa1", "--env-file", path)
			require.ErrorContains(t, err, tc.want)
			assert.Equal(t, 1, output.ExitCode(err))

			if tc.status == 0 && !tc.clientFailure {
				var ee *base.ExitError
				require.ErrorAs(t, err, &ee)
				assert.Equal(t, 1, ee.Code)
			}

			if tc.occupied {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, "existing", string(data))
			} else {
				_, err := os.Stat(path)
				require.ErrorIs(t, err, os.ErrNotExist)
			}

			if tc.occupied || tc.clientFailure {
				assert.Empty(t, f.requests())
			} else {
				assert.Len(t, f.requests(), 1)
			}

			assert.Empty(t, out)
			assert.Empty(t, stderr)
		})
	}
}

func TestRolesListFiltersAndFieldAliases(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{"", "S"} {
		for _, tc := range []struct{ name, fields string }{
			{"snake", `"role_name":"vpc.admin","subject_id":"S","subject_type":"service_account"`},
			{"camel", `"roleName":"vpc.admin","subjectId":"S","subjectType":"service_account"`},
			{"role id fallback", `"role_id":"vpc.admin","subject_id":"S","subject_type":"service_account"`},
		} {
			for _, json := range []bool{false, true} {
				t.Run(subject+"/"+tc.name+map[bool]string{false: "/table", true: "/json"}[json], func(t *testing.T) {
					t.Parallel()

					raw := `{"permissions":[{"id":"pm1",` + tc.fields + `,"enabled":true,"future":17}]}`
					f := newFake(map[string]reply{"GET /api/v3/permissions": {body: raw}})
					args := []string{"roles", "list"}

					filter := "resource_id=P"
					if subject != "" {
						args = append(args, "--subject", subject)
						filter += " AND subject_id=S"
					}

					out, stderr, err := run(t, f, json, nil, args...)
					require.NoError(t, err)

					if json {
						assert.JSONEq(t, raw, out)
					} else {
						assert.Equal(t, []string{"ID", "ROLE", "SUBJECT", "SUBJECT_TYPE", "ENABLED", "pm1", "vpc.admin", "S", "service_account", "true"}, strings.Fields(out))
					}

					assert.Empty(t, stderr)
					assert.Equal(t, []request{{key: "GET /api/v3/permissions", query: url.Values{"filter": {filter}, "pageSize": {"1000"}}}}, f.requests())
				})
			}
		}
	}
}

func TestGrantRole(t *testing.T) {
	t.Parallel()

	for _, expires := range []string{"", "2026-12-31T00:00:00Z"} {
		for _, json := range []bool{false, true} {
			t.Run(expires+map[bool]string{false: "/human", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()

				const raw = `{"id":"pm1","future":17}`

				f := newFake(map[string]reply{"POST /api/v3/permissions": {body: raw}})

				args := []string{"roles", "grant", "--role", "vpc.admin", "--subject", "S"}
				if expires != "" {
					args = append(args, "--expires", expires)
				}

				out, stderr, err := run(t, f, json, nil, args...)
				require.NoError(t, err)

				if json {
					assert.JSONEq(t, raw, out)
				} else {
					assert.Equal(t, "assigned vpc.admin (id pm1)\n", out)
				}

				assert.Empty(t, stderr)

				calls := f.requests()
				require.Len(t, calls, 1)
				assert.Equal(t, "POST /api/v3/permissions", calls[0].key)
				assert.Empty(t, calls[0].query)

				want := `{"resourceId":"P","subjectId":"S","role":"vpc.admin"}`
				if expires != "" {
					want = `{"resourceId":"P","subjectId":"S","role":"vpc.admin","expiresAt":"2026-12-31T00:00:00Z"}`
				}

				assert.JSONEq(t, want, calls[0].body)
			})
		}
	}
}

func TestIAMRefusesBeforeRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, want string
		args       []string
		noProject  bool
	}{
		{name: "account list project", args: []string{"sa", "list"}, noProject: true, want: "project not set"},
		{name: "account create project", args: []string{"sa", "create", "--name", "robot"}, noProject: true, want: "project not set"},
		{name: "role list project", args: []string{"roles", "list"}, noProject: true, want: "project not set"},
		{name: "grant project", args: []string{"roles", "grant", "--role", "vpc.admin", "--subject", "S"}, noProject: true, want: "project not set"},
		{name: "key project", args: []string{"access-keys", "create", "sa1", "--env-file", "unused.env"}, noProject: true, want: "project not set"},
		{name: "account flags", args: []string{"sa", "create"}, want: `required flag(s) "name" not set`},
		{name: "key flags", args: []string{"access-keys", "create", "sa1"}, want: `required flag(s) "env-file" not set`},
		{name: "grant flags", args: []string{"roles", "grant"}, want: `required flag(s) "role", "subject" not set`},
		{name: "invalid expiry", args: []string{"roles", "grant", "--role", "vpc.admin", "--subject", "S", "--expires", "2026-12-31"}, want: "expiry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(nil)
			out, stderr, err := run(t, f, false, func(s *state.State) {
				if tc.noProject {
					s.Project = ""
				}
			}, tc.args...)
			require.ErrorContains(t, err, tc.want)
			assert.Equal(t, 1, output.ExitCode(err))

			if tc.noProject {
				require.ErrorIs(t, err, state.ErrNoProject)
			}

			if tc.name == "invalid expiry" {
				require.ErrorIs(t, err, cloud.ErrBadExpires)
			}

			assert.Empty(t, f.requests())
			assert.Empty(t, out)
			assert.Empty(t, stderr)
		})
	}
}

func TestIAMClientAndAPIErrors(t *testing.T) {
	t.Parallel()

	clientErr := errors.New("iam unavailable")
	for _, args := range [][]string{
		{"sa", "list"},
		{"sa", "get", "sa1"},
		{"sa", "create", "--name", "robot"},
		{"sa", "delete", "sa1", "--yes"},
		{"access-keys", "list", "sa1"},
		{"access-keys", "get", "sa1", "ak1"},
		{"access-keys", "delete", "sa1", "ak1", "--yes"},
		{"roles", "list"},
		{"roles", "get", "pm1"},
		{"roles", "grant", "--role", "vpc.admin", "--subject", "S"},
		{"roles", "revoke", "pm1", "--yes"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			for _, clientFailure := range []bool{false, true} {
				f := newFake(nil)

				out, stderr, err := run(t, f, false, func(s *state.State) {
					if clientFailure {
						s.IAM = func() (*cloud.IAM, error) { return nil, clientErr }
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
				assert.Empty(t, stderr)
			}
		})
	}
}

func TestIAMFormattingHelpersHandleMissingOrWrongTypes(t *testing.T) {
	t.Parallel()
	assert.Nil(t, items([]byte(`{"access_keys":{}}`), "access_keys", "accessKeys"))
	assert.Empty(t, pick("not an object", "id"))
	assert.Equal(t, "fallback", pick(map[string]any{"id": 17, "empty": "", "name": "fallback"}, "id", "empty", "name"))
}
