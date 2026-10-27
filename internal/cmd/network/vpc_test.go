package network

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

func TestVPCListAndGet(t *testing.T) {
	t.Parallel()

	for _, get := range []bool{false, true} {
		for _, json := range []bool{false, true} {
			t.Run(map[bool]string{false: "list", true: "get"}[get]+map[bool]string{false: "/human", true: "/json"}[json], func(t *testing.T) {
				t.Parallel()

				raw := `{"vpcs":[{"id":"v1","name":"Default","default":true,"future":17}]}`
				key := "GET /v1/vpcs"
				args := []string{"vpcs", "list"}

				query := url.Values{"projectId": {testProject}, "pageSize": {"1000"}}
				if get {
					raw = `{"id":"v1","name":"Default","future":17}`
					key += "/v1"
					args = []string{"vpcs", "get", "v1"}
					query = url.Values{}
				}

				f := newFake(map[string][]reply{key: {{body: raw}}})
				out, _, err := run(t, f, json, nil, args...)
				require.NoError(t, err)

				if json || get {
					assert.JSONEq(t, raw, out)
				} else {
					assert.Equal(t, []string{"ID", "NAME", "DEFAULT", "v1", "Default", "true"}, strings.Fields(out))
				}

				assert.Equal(t, []request{{key: key, query: query}}, f.requests())
			})
		}
	}
}

func TestVPCMutationsAndOperations(t *testing.T) {
	t.Parallel()

	for _, create := range []bool{false, true} {
		for _, json := range []bool{false, true} {
			for _, tc := range []struct {
				name, initial, final, wantErr string
				wait                          bool
				code                          int
			}{
				{name: "no wait", initial: `{"id":"op1","resourceId":"v1"}`},
				{name: "wait", initial: `{"id":"op1"}`, final: `{"id":"op1","resourceId":"v1","done":true}`, wait: true},
				{name: "failed", initial: `{"id":"op1"}`, final: `{"id":"op1","done":true,"error":{"message":"vpc has subnets"}}`, wait: true, code: 1, wantErr: "vpc has subnets"},
				{name: "no operation", initial: `{}`, wait: true, code: 8, wantErr: "API response has no operation"},
				{name: "poll fails without hint", initial: `{"id":"op1"}`, wait: true, code: 8, wantErr: "no route"},
			} {
				t.Run(map[bool]string{false: "delete", true: "create"}[create]+map[bool]string{false: "/human/", true: "/json/"}[json]+tc.name, func(t *testing.T) {
					t.Parallel()

					key := "DELETE /v1/vpcs/v1"

					args := []string{"vpcs", "delete", "v1", "--yes"}
					if create {
						key = "POST /v1/vpcs"
						args = []string{"vpcs", "create", "--name", "private", "--description", "private db"}
					}

					if tc.wait {
						args = append(args, "--wait")
					}

					routes := map[string][]reply{key: {{body: tc.initial}}}
					if tc.final != "" {
						routes["GET /v1/vpcs/operations/op1"] = []reply{{body: `{"id":"op1","done":false}`}, {body: tc.final}}
					}

					f := newFake(routes)
					out, stderr, err := run(t, f, json, nil, args...)
					assert.Equal(t, tc.code, output.ExitCode(err))

					if tc.wantErr != "" {
						require.ErrorContains(t, err, tc.wantErr)
						assert.NotContains(t, err.Error(), "operations get")
					} else {
						require.NoError(t, err)
					}

					if tc.name == "no operation" {
						var ee *base.ExitError
						require.ErrorAs(t, err, &ee)
						assert.Equal(t, 8, ee.Code)
					}

					wantRaw := tc.initial
					if tc.final != "" {
						wantRaw = tc.final
					}

					if json {
						assert.JSONEq(t, wantRaw, out)
					} else {
						switch tc.name {
						case "no wait":
							assert.Contains(t, out, "operation op1 — in progress")
						case "wait":
							assert.Contains(t, out, "operation op1 — completed")
							assert.Contains(t, out, "resource  v1")
						case "failed":
							assert.Contains(t, out, "operation op1 — failed: vpc has subnets")
						default:
							assert.Empty(t, out)
						}
					}

					assert.Empty(t, stderr)

					calls := f.requests()

					count := 1
					if tc.final != "" {
						count += 2
					} else if tc.name == "poll fails without hint" {
						count++
					}

					require.Len(t, calls, count)
					assert.Equal(t, key, calls[0].key)
					assert.Empty(t, calls[0].query)

					if create {
						assert.JSONEq(t, `{"name":"private","description":"private db","projectId":"11111111-1111-4111-8111-111111111111"}`, calls[0].body)
					} else {
						assert.Empty(t, calls[0].body)
					}

					for _, call := range calls[1:] {
						assert.Equal(t, request{key: "GET /v1/vpcs/operations/op1", query: url.Values{}}, call)
					}
				})
			}
		}
	}
}
