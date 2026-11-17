package cluster

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// Отказ в клиенте, битый ответ и локальная ошибка ввода не превращаются в пустой успех.
// Сетка содержит корректные ответы; таблица границ не проверяет эти отказы.
func TestClusterFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, args, route, raw, want string
		setup                        func(*state.State, *cobra.Command)
	}{
		{"list project", "list", "", "", "project", func(s *state.State, _ *cobra.Command) { s.Project = "" }},
		{"create project", createArgs, "", "", "project", func(s *state.State, _ *cobra.Command) { s.Project = "" }},
		{"list client", "list", "", "", "unavailable", unavailable},
		{"get client", "get c1", "", "", "unavailable", unavailable},
		{"create client", createArgs, "", "", "unavailable", unavailable},
		{"update client", "update c1 --data-nodes 3", "", "", "unavailable", unavailable},
		{"list API", "list", "", "", "API 404", nil},
		{"get API", "get c1", "", "", "API 404", nil},
		{"list schema", "list", "GET /v1/clusters", `{"clusters":true}`, "not a list", nil},
		{"get schema", "get c1", "GET /v1/clusters/c1", `{"data_nodes":"bad"}`, "parse response", nil},
		{"create body", createArgs + " --body []", "", "", "--body", nil},
		{"create topology", createArgs + ` --body {"data_nodes":"bad"}`, "", "", "--body", nil},
		{"create API", createArgs, "GET /v1/specifications/s", validSpec, "API 404", nil},
		{"update unchanged", "update c1", "", "", "Specify fields", nil},
		{"update body", "update c1 --body []", "", "", "--body", nil},
		{"update topology", `update c1 --body {"data_nodes":"bad"}`, "", "", "--body", nil},
		{"update cluster API", "update c1 --data-nodes 3", "", "", "API 404", nil},
		{"update cluster schema", "update c1 --data-nodes 3", "GET /v1/clusters/c1", `{"data_nodes":"bad"}`, "parse response", nil},
		{"update API", "update c1 --snapshots-enabled=false", "", "", "API 404", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			routes := map[string]string{}
			if tc.route != "" {
				routes[tc.route] = tc.raw
			}

			f := newFake(routes)
			out, _, err := runWith(t, f, false, tc.setup, strings.Fields(tc.args)...)
			require.ErrorContains(t, err, tc.want)
			assert.Empty(t, out)
		})
	}
}

func unavailable(s *state.State, _ *cobra.Command) {
	s.OpenSearch = func() (*cloud.OpenSearch, error) { return nil, errors.New("unavailable") }
}

// --wait должен опрашивать OpenSearch и после успеха выводить кластер, включая адреса hosts.
// Сетка создаёт без ожидания и не видит ошибку маршрута после завершения операции.
func TestCreateWaitFetchesOpenSearchCluster(t *testing.T) {
	t.Parallel()

	for _, result := range []string{"success", "cluster API", "operation failed"} {
		t.Run(result, func(t *testing.T) {
			t.Parallel()

			routes := map[string]string{"GET /v1/specifications/s": validSpec, "POST /v1/clusters": `{"id":"op1","done":false,"resource_id":"c1"}`, "GET /v1/operations/op1": `{"id":"op1","done":true,"resource_id":"c1"}`, "GET /v1/clusters/c1": `{"id":"c1","name":"created","hosts":["os:9200"],"data_nodes":3,"master_nodes":3}`}
			if result == "cluster API" {
				delete(routes, "GET /v1/clusters/c1")
			}

			if result == "operation failed" {
				routes["GET /v1/operations/op1"] = `{"id":"op1","done":true,"error":{"code":13,"message":"failed operation"}}`
			}

			f := newFake(routes)

			out, err := run(t, f, false, append(strings.Fields(createArgs), "--wait")...)
			if result == "success" {
				require.NoError(t, err)
				assert.Contains(t, out, "created")
				assert.Contains(t, out, "os:9200")
			} else {
				require.Error(t, err)

				if result == "operation failed" {
					assert.Contains(t, out, "failed operation")
				} else {
					assert.Empty(t, out)
				}
			}

			assert.Contains(t, f.requests(), "GET /v1/operations/op1")
		})
	}
}

// Выключение restore с идентификаторами не должно терять enabled:false; другие уровни восстанавливают с enabled:true.
func TestCreateDisabledRestore(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"GET /v1/specifications/s": validSpec, "POST /v1/clusters": `{"id":"op"}`})

	args := append(strings.Fields(createArgs), "--restore=false", "--restore-cluster-id", "source", "--restore-snapshot", "daily")
	_, err := run(t, f, true, args...)
	require.NoError(t, err)
	assert.Contains(t, f.body("POST /v1/clusters"), `"restore":{"cluster_id":"source","enabled":false,"snapshot_name":"daily"}`)
}

// Необязательные мастер-поля не получают придуманных defaults; неизвестные границы не подменяются нулём.
func TestCreateOptionalSpecificationFields(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"GET /v1/specifications/body": `{}`, "POST /v1/clusters": `{"id":"op"}`})
	out, msg, err := runWith(t, f, true, nil, "create", "--name", "n", "--version", "2.18.0", "--spec", "flags", "--data-nodes", "7", "--data-storage-gib", "1", "--body", `{"specification_id":"body"}`)
	require.NoError(t, err)
	assert.Contains(t, out, `"op"`)
	assert.Empty(t, msg)
	assert.NotContains(t, f.body("POST /v1/clusters"), `"master_nodes"`)
	assert.Equal(t, []string{"GET /v1/specifications/body", "POST /v1/clusters"}, f.requests())
}

// Ответ list может опускать необязательные master/subnet поля; битый размер узла должен дать ошибку.
// Golden содержит все подсети и мастер-узлы, а cloud не разбирает схему human-таблицы.
func TestListPartialCluster(t *testing.T) {
	t.Parallel()

	raw := `{"clusters":[{"id":"partial","name":"n","status":"READY","health":"HEALTHY","version":"2.18.0","data_nodes":3}]}`
	f := newFake(map[string]string{"GET /v1/clusters": raw})
	out, err := run(t, f, false, "list")
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, []string{"partial", "n", "READY", "HEALTHY", "2.18.0", "3", "0"}, strings.Fields(lines[1]))
	assert.Equal(t, []string{"GET /v1/clusters"}, f.requests())
	f = newFake(map[string]string{"GET /v1/clusters": `{"clusters":[{"data_nodes":"bad"}]}`})
	out, err = run(t, f, false, "list")
	require.ErrorContains(t, err, "parse response")
	assert.Empty(t, out)
}

// Подсказка запрета удаления во время создания должна указывать именно OpenSearch; общий base не знает продукт.
func TestDeleteDeployingHint(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"DELETE /v1/clusters/c1": `{"message":"create inventory task: FailedPrecondition"}`})
	f.statuses = map[string]int{"DELETE /v1/clusters/c1": 400}
	out, err := run(t, f, false, "delete", "c1", "--yes")
	require.ErrorContains(t, err, "opensearch clusters get c1")
	assert.Empty(t, out)
	assert.Equal(t, []string{"DELETE /v1/clusters/c1"}, f.requests())
}

// Живой вызов 16.11: без SNAT-шлюза и без DNS-сервера VPC API отвечает 400 с текстом gRPC,
// из которого не понять, что делать. Ловит потерю подсказки; прочие 400 и не-400 идут как есть.
func TestCreateNetworkHint(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, reply string
		status      int
		want, not   string
	}{
		{"nat", `{"message":"validate subnets: rpc error: code = FailedPrecondition desc = appropriate public nat gateways don't exist"}`, 400, "SNAT", ""},
		{"dns", `{"message":"validate subnets: rpc error: code = FailedPrecondition desc = all subnets must point to the only one active DNS server at the same VPC"}`, 400, "DNS server in the VPC", ""},
		{"other 400", `{"message":"name is taken"}`, 400, "name is taken", "SNAT"},
		{"not 400", `{"message":"nat gateway backend down"}`, 500, "API 500", "SNAT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"GET /v1/specifications/s": validSpec, "POST /v1/clusters": tc.reply})
			f.statuses = map[string]int{"POST /v1/clusters": tc.status}
			_, err := run(t, f, false, strings.Fields(createArgs)...)
			require.ErrorContains(t, err, tc.want)
			require.ErrorContains(t, err, "API ")

			if tc.not != "" {
				assert.NotContains(t, err.Error(), tc.not)
			}
		})
	}
}
