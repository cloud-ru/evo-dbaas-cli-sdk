package cluster

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

func TestCreateTopology(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, subnets string
		brokers       int
		controllers   bool
		want          string
	}{
		{"нет подсетей", "", 1, false, "requires 1 or 3 subnets"},
		{"две подсети", "s1,s2", 1, false, "requires 1 or 3 subnets"},
		{"четыре подсети", "s1,s2,s3,s4", 1, true, "requires 1 or 3 subnets"},
		{"single один", "s1", 1, false, ""},
		{"single три", "s1", 3, false, ""},
		{"single ноль", "s1", 0, false, "1 or 3 brokers in each zone"},
		{"single два", "s1", 2, false, "1 or 3 brokers in each zone"},
		{"single четыре", "s1", 4, false, "1 or 3 brokers in each zone"},
		{"single dedicated один", "s1", 1, true, "dedicated controllers require 3 subnets"},
		{"single dedicated три", "s1", 3, true, "dedicated controllers require 3 subnets"},
		{"single dedicated два", "s1", 2, true, "dedicated controllers require 3 subnets"},
		{"multi combined один", "s1,s2,s3", 1, false, ""},
		{"multi combined ноль", "s1,s2,s3", 0, false, "require 1 broker in each zone"},
		{"multi combined два", "s1,s2,s3", 2, false, "require 1 broker in each zone"},
		{"multi combined три", "s1,s2,s3", 3, false, "require 1 broker in each zone"},
		{"multi dedicated один", "s1,s2,s3", 1, true, ""},
		{"multi dedicated два", "s1,s2,s3", 2, true, ""},
		{"multi dedicated три", "s1,s2,s3", 3, true, ""},
		{"multi dedicated четыре", "s1,s2,s3", 4, true, ""},
		{"multi dedicated пять", "s1,s2,s3", 5, true, ""},
		{"multi dedicated ноль", "s1,s2,s3", 0, true, "1–5 brokers in each zone"},
		{"multi dedicated минус", "s1,s2,s3", -1, true, "1–5 brokers in each zone"},
		{"multi dedicated шесть", "s1,s2,s3", 6, true, "1–5 brokers in each zone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := []string{
				"create", "--name", "events", "--version-id", "v1", "--spec", "s1", "--storage-gib", "40",
				"--subnet-id", tc.subnets, "--brokers", fmt.Sprint(tc.brokers),
			}
			if tc.controllers {
				args = append(args, "--controller-spec", "cs", "--controller-storage-gib", "10")
			}

			f := newFake(map[string]string{"POST /v1/clusters": `{"id":"op1","done":false}`})

			_, err := run(t, f, false, args...)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.Empty(t, f.requests())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, []string{"POST /v1/clusters"}, f.requests())

			var body map[string]any
			require.NoError(t, output.Decode([]byte(f.body("POST /v1/clusters")), &body))
			assert.NotContains(t, body, "subnet_id")
			assert.Contains(t, body, "subnet_ids")
			assert.EqualValues(t, tc.brokers, body["brokers"])

			if tc.controllers {
				assert.Equal(t, map[string]any{"specification_id": "cs", "storage_gib": float64(10)}, body["controller_spec"])
			} else {
				assert.NotContains(t, body, "controller_spec")
			}
		})
	}
}

func TestCreateControllerPair(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		flags []string
	}{
		{"только спецификация", []string{"--controller-spec", "cs"}},
		{"только диск", []string{"--controller-storage-gib", "10"}},
		{"нулевой диск", []string{"--controller-spec", "cs", "--controller-storage-gib", "0"}},
		{"отрицательный диск", []string{"--controller-spec", "cs", "--controller-storage-gib", "-1"}},
		{"пустая спецификация", []string{"--controller-spec", "", "--controller-storage-gib", "10"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"create", "--name", "events", "--version-id", "v1", "--spec", "s1", "--storage-gib", "40", "--subnet-id", "s1"}, tc.flags...)
			f := newFake(nil)
			_, err := run(t, f, false, args...)
			require.ErrorContains(t, err, "--controller-spec and --controller-storage-gib must be specified together")
			assert.Empty(t, f.requests())
		})
	}
}

func TestCreateBodyTopology(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, subnets            string
		brokers                  int
		controllers              bool
		body, field, value, want string
	}{
		{"body исправляет brokers", "s1", 2, false, `{"brokers":1}`, "brokers", `1`, ""},
		{"body нарушает brokers", "s1", 1, false, `{"brokers":2}`, "", ``, "1 or 3 brokers in each zone"},
		{"body исправляет subnets", "s1,s2", 1, false, `{"subnet_ids":["s1"]}`, "subnet_ids", `["s1"]`, ""},
		{"body нарушает subnets", "s1", 1, false, `{"subnet_ids":["s1","s2"]}`, "", ``, "requires 1 or 3 subnets"},
		{"body включает dedicated", "s1,s2,s3", 2, false, `{"controller_spec":{"specification_id":"cs","storage_gib":10}}`, "controller_spec", `{"specification_id":"cs","storage_gib":10}`, ""},
		{"body dedicated в single", "s1", 1, false, `{"controller_spec":{"specification_id":"cs","storage_gib":10}}`, "", ``, "dedicated controllers require 3 subnets"},
		{"body исправляет dedicated subnets", "s1", 2, true, `{"subnet_ids":["s1","s2","s3"]}`, "subnet_ids", `["s1","s2","s3"]`, ""},
		{"неверный тип brokers", "s1", 2, false, `{"brokers":"api"}`, "brokers", `"api"`, ""},
		{"null controller", "s1", 1, true, `{"controller_spec":null}`, "controller_spec", `null`, ""},
		{"неверный brokers не скрывает subnets", "s1,s2", 1, false, `{"brokers":null}`, "", ``, "requires 1 or 3 subnets"},
		{"неверный controller не скрывает subnets", "s1,s2", 1, false, `{"controller_spec":false}`, "", ``, "requires 1 or 3 subnets"},
		{"неверный brokers не скрывает dedicated", "s1", 1, false, `{"brokers":null,"controller_spec":{}}`, "", ``, "dedicated controllers require 3 subnets"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := []string{"create", "--name", "events", "--version-id", "v1", "--spec", "s1", "--storage-gib", "40", "--subnet-id", tc.subnets, "--brokers", fmt.Sprint(tc.brokers), "--body", tc.body}
			if tc.controllers {
				args = append(args, "--controller-spec", "cs", "--controller-storage-gib", "10")
			}

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
