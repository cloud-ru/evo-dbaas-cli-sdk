package cluster

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateKeeperTopology(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, mode, subnets string
		shards, replicas    int
		valid               bool
	}{
		{"embedded без подсети", "embedded", "", 1, 1, false},
		{"standalone шард 1", "standalone", "s1,s2,s3", 1, 3, true},
		{"standalone шард 2", "standalone", "s1,s2,s3", 2, 3, true},
		{"standalone шард 3", "standalone", "s1,s2,s3", 3, 3, true},
		{"standalone шард 0", "standalone", "s1,s2,s3", 0, 3, false},
		{"standalone шард 4", "standalone", "s1,s2,s3", 4, 3, false},
		{"standalone реплик 0", "standalone", "s1,s2,s3", 1, 0, false},
		{"standalone реплик 1", "standalone", "s1,s2,s3", 1, 1, false},
		{"standalone реплик 2", "standalone", "s1,s2,s3", 1, 2, false},
		{"standalone реплик 4", "standalone", "s1,s2,s3", 1, 4, false},
		{"standalone подсетей 0", "standalone", "", 1, 3, false},
		{"standalone подсетей 1", "standalone", "s1", 1, 3, false},
		{"standalone подсетей 2", "standalone", "s1,s2", 1, 3, false},
		{"standalone подсетей 4", "standalone", "s1,s2,s3,s4", 1, 3, false},
		{"standalone дубликат 1 2", "standalone", "s1,s1,s3", 1, 3, false},
		{"standalone дубликат 1 3", "standalone", "s1,s2,s1", 1, 3, false},
		{"standalone дубликат 2 3", "standalone", "s1,s2,s2", 1, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := []string{
				"create", "--name", "demo", "--version", "25.8", "--spec", "s1", "--keeper", tc.mode,
				"--shards", fmt.Sprint(tc.shards), "--replicas", fmt.Sprint(tc.replicas), "--subnet-id", tc.subnets,
			}
			if tc.mode == "standalone" {
				args = append(args, "--keeper-spec", "ks", "--keeper-storage-gib", "10")
			}

			f := newFake(map[string]string{"POST /v1/clusters": `{"id":"op1","done":false}`})

			_, err := run(t, f, false, args...)
			if !tc.valid {
				want := "--keeper embedded requires exactly 1 shard, 1 replica and 1 subnet"
				if tc.mode == "standalone" {
					want = "--keeper standalone requires 1–3 shards, 3 replicas and 3 distinct subnets"
				}

				require.ErrorContains(t, err, want)
				assert.Empty(t, f.requests())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, []string{"POST /v1/clusters"}, f.requests())
			assert.JSONEq(t, fmt.Sprintf(`{"project_id":"p1","name":"demo","version":"25.8","specification_id":"s1","keeper_mode":"KEEPER_MODE_STANDALONE","standalone_keeper":{"specification_id":"ks","storage_gib":10},"shards":%d,"replicas":3,"subnet_ids":["s1","s2","s3"]}`, tc.shards), f.body("POST /v1/clusters"))
		})
	}
}

func TestCreateBodyTopology(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                     string
		flags                    []string
		body, field, value, want string
	}{
		{"body исправляет replicas", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3", "--replicas", "1"}, `{"replicas":3}`, "replicas", `3`, ""},
		{"body нарушает replicas", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3"}, `{"replicas":1}`, "", ``, "requires 1\u20133 shards"},
		{"body исправляет shards", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3", "--shards", "0"}, `{"shards":2}`, "shards", `2`, ""},
		{"body нарушает shards", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3"}, `{"shards":4}`, "", ``, "requires 1\u20133 shards"},
		{"body исправляет subnets", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2", "--replicas", "3"}, `{"subnet_ids":["s1","s2","s3"]}`, "subnet_ids", `["s1","s2","s3"]`, ""},
		{"body нарушает subnets", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3"}, `{"subnet_ids":["s1","s2","s1"]}`, "", ``, "requires 1\u20133 shards"},
		{"body задаёт keeper mode", []string{}, `{"keeper_mode":"KEEPER_MODE_STANDALONE","standalone_keeper":{"specification_id":"ks","storage_gib":10},"replicas":3,"subnet_ids":["s1","s2","s3"]}`, "keeper_mode", `"KEEPER_MODE_STANDALONE"`, ""},
		{"body заменяет keeper mode", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3"}, `{"keeper_mode":"KEEPER_MODE_EMBEDDED","standalone_keeper":{},"replicas":1,"subnet_ids":["s1"]}`, "keeper_mode", `"KEEPER_MODE_EMBEDDED"`, ""},
		{"body заменяет keeper целиком", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3"}, `{"standalone_keeper":{"storage_gib":10}}`, "", ``, "requires --keeper-spec"},
		{"body задаёт keeper", []string{"--keeper", "standalone", "--subnet-id", "s1,s2,s3", "--replicas", "3"}, `{"standalone_keeper":{"specification_id":"ks","storage_gib":10}}`, "standalone_keeper", `{"specification_id":"ks","storage_gib":10}`, ""},
		{"неверный тип replicas", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3", "--replicas", "1"}, `{"replicas":false}`, "replicas", `false`, ""},
		{"неверный тип mode", []string{"--keeper", "embedded", "--subnet-id", "s1,s2"}, `{"keeper_mode":false}`, "keeper_mode", `false`, ""},
		{"неверный тип keeper spec", []string{"--keeper", "standalone", "--subnet-id", "s1,s2,s3", "--replicas", "3"}, `{"standalone_keeper":{"specification_id":false,"storage_gib":10}}`, "standalone_keeper", `{"specification_id":false,"storage_gib":10}`, ""},
		{"неверный shards не скрывает replicas", []string{"--keeper", "standalone", "--keeper-spec", "ks", "--subnet-id", "s1,s2,s3", "--replicas", "3"}, `{"shards":null,"replicas":1}`, "", ``, "requires 1\u20133 shards"},
		{"body keeper storage в embedded", []string{"--keeper", "embedded", "--subnet-id", "s1"}, `{"standalone_keeper":{"storage_gib":10}}`, "", ``, "require --keeper standalone"},
		{"неверный keeper storage не скрывает spec", []string{"--keeper", "embedded", "--subnet-id", "s1"}, `{"standalone_keeper":{"specification_id":"ks","storage_gib":"api"}}`, "", ``, "require --keeper standalone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"create", "--name", "demo", "--version", "25.8", "--spec", "s1", "--body", tc.body}, tc.flags...)
			f := newFake(map[string]string{"POST /v1/clusters": `{"id":"op1","done":false}`})

			_, err := run(t, f, false, args...)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.Empty(t, f.requests())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, []string{"POST /v1/clusters"}, f.requests())

			var sent map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(f.body("POST /v1/clusters")), &sent))
			assert.JSONEq(t, tc.value, string(sent[tc.field]))
		})
	}
}
