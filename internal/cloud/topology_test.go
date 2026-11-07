package cloud

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKafkaTopology(t *testing.T) {
	t.Parallel()

	one, three, empty := []string{"s1"}, []string{"s1", "s2", "s3"}, []string{}
	for _, tc := range []struct {
		name, body string
		want       KafkaTopology
		err        bool
	}{
		{"флаги без body", "", KafkaTopology{ptr(int32(3)), &one, ptr(false)}, false},
		{"body заменяет поля", `{"brokers":2,"subnet_ids":["s1","s2","s3"],"controller_spec":{}}`, KafkaTopology{ptr(int32(2)), &three, ptr(true)}, false},
		{"неверный тип подсетей", `{"subnet_ids":"api"}`, KafkaTopology{ptr(int32(3)), nil, ptr(false)}, false},
		{"неверный элемент подсетей", `{"subnet_ids":["s1",null]}`, KafkaTopology{ptr(int32(3)), nil, ptr(false)}, false},
		{"null и пустой массив", `{"brokers":null,"subnet_ids":[],"controller_spec":null}`, KafkaTopology{nil, &empty, nil}, false},
		{"неверные число и объект", `{"brokers":false,"controller_spec":false}`, KafkaTopology{nil, &one, nil}, false},
		{"ошибка проекции body", `broken`, KafkaTopology{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := KafkaClusterCreate{Brokers: 3, Subnets: one}
			if tc.body != "" {
				c.Body = []byte(tc.body)
			}

			got, err := c.Topology()
			if tc.err {
				require.ErrorContains(t, err, "--body")
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClickHouseTopology(t *testing.T) {
	t.Parallel()

	one, three := []string{"s1"}, []string{"s1", "s2", "s3"}
	for _, tc := range []struct {
		name, body string
		want       ClickHouseTopology
		err        bool
	}{
		{"флаги без body", "", ClickHouseTopology{ptr(int32(2)), ptr(int32(3)), ptr(int32(10)), &three, ptr("standalone"), ptr("ks")}, false},
		{"body и полная замена Keeper", `{"shards":1,"replicas":1,"subnet_ids":["s1"],"keeper_mode":"KEEPER_MODE_EMBEDDED","standalone_keeper":{"specification_id":"body"}}`, ClickHouseTopology{ptr(int32(1)), ptr(int32(1)), ptr(int32(0)), &one, ptr("EMBEDDED"), ptr("body")}, false},
		{"legacy из body", `{"keeper_mode":"KEEPER_MODE_UNSPECIFIED"}`, ClickHouseTopology{ptr(int32(2)), ptr(int32(3)), ptr(int32(10)), &three, ptr(""), ptr("ks")}, false},
		{"неверные shards subnets Keeper", `{"shards":"api","subnet_ids":false,"standalone_keeper":false}`, ClickHouseTopology{nil, ptr(int32(3)), nil, nil, ptr("standalone"), nil}, false},
		{"неверные mode replicas поля Keeper", `{"keeper_mode":false,"replicas":null,"standalone_keeper":{"specification_id":false,"storage_gib":"api"}}`, ClickHouseTopology{ptr(int32(2)), nil, nil, &three, nil, nil}, false},
		{"ошибка проекции body", `[]`, ClickHouseTopology{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := ClickHouseClusterCreate{Shards: 2, Replicas: 3, Subnets: three, KeeperMode: "standalone", Keeper: ClickHouseKeeper{Spec: "ks", StorageGiB: 10}}
			if tc.body != "" {
				c.Body = []byte(tc.body)
			}

			got, err := c.Topology()
			if tc.err {
				require.ErrorContains(t, err, "--body")
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}
