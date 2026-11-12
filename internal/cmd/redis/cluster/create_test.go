package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

// Находка 12.11: две подсети master-replica дают API 400, три разные работают.
// Проверка должна отказать локально; фейк отвергает то же неверное тело.
func TestCreateMultiAZSubnets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, subnets string
		valid         bool
	}{
		{"без подсетей", "", true},
		{"одна подсеть", "s1", true},
		{"две подсети", "s1,s2", false},
		{"четыре подсети", "s1,s2,s3,s4", false},
		{"повтор первой и второй", "s1,s1,s3", false},
		{"повтор первой и третьей", "s1,s2,s1", false},
		{"повтор второй и третьей", "s1,s2,s2", false},
		{"три разные подсети", "s1,s2,s3", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"POST /v1/clusters": `{"id":"op1","done":false}`})

			args := []string{"create", "--name", "n", "--version-id", "v", "--spec", "s", "--type", "master-replica"}
			if tc.subnets != "" {
				args = append(args, "--subnet-id", tc.subnets)
			}

			out, err := run(t, f, false, args...)
			if !tc.valid {
				var exit *base.ExitError
				require.ErrorAs(t, err, &exit)
				assert.Equal(t, 1, output.ExitCode(err))
				assert.Equal(t, "Redis Multi-AZ requires exactly 3 distinct subnet ids in --subnet-id", exit.Msg)
				assert.Empty(t, f.bodies)
				assert.Empty(t, out)

				return
			}

			require.NoError(t, err)
			assert.Contains(t, out, "op1")

			body := f.bodies["POST /v1/clusters"]
			assert.Contains(t, body, `"replicas":1`)
			assert.Contains(t, body, `"type":"MASTER_REPLICA"`)

			if tc.subnets == "s1,s2,s3" {
				assert.Contains(t, body, `"subnet_ids":["s1","s2","s3"]`)
			}
		})
	}
}
