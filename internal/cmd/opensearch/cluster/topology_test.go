package cluster

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	validCluster = `{"id":"c1","specification_id":"s","data_nodes":3,"master_nodes":3,"data_nodes_disk_size_gib":100,"master_nodes_disk_size_gib":25}`
	validSpec    = `{"id":"s","min_data_nodes":2,"max_data_nodes":5,"min_master_nodes":2,"max_master_nodes":4,"min_data_nodes_storage_gigs":40,"min_master_nodes_storage_gigs":10}`
	createArgs   = "create --name n --version 2.18.0 --spec s --data-nodes 3 --master-nodes 3 --data-storage-gib 100 --master-storage-gib 25"
)

// Каждая граница × найденная/отсутствующая/недоступная спецификация × create/update × флаги/body.
// Ловит off-by-one, обход через --body и отправку PATCH до проверки; сетка проверяет штатные вызовы.
func TestClusterSpecificationLimits(t *testing.T) {
	t.Parallel()

	for _, rule := range []struct {
		flag, field, label string
		min, max           int
	}{
		{"data-nodes", "data_nodes", "data nodes", 2, 5},
		{"master-nodes", "master_nodes", "master nodes", 2, 4},
		{"data-storage-gib", "data_nodes_disk_size_gib", "data node disk size", 40, 0},
		{"master-storage-gib", "master_nodes_disk_size_gib", "master node disk size", 10, 0},
	} {
		// У дисков нет максимума в спеке: проверяем min−1/min/min+1 и наибольший int32.
		values := []int{rule.min - 1, rule.min, rule.max, rule.max + 1}
		if rule.max == 0 {
			values = []int{rule.min - 1, rule.min, rule.min + 1, math.MaxInt32}
		}

		for _, value := range values {
			for _, lookup := range []string{"found", "missing", "failed"} {
				for _, verb := range []string{"create", "update"} {
					for _, source := range []string{"flag", "body"} {
						t.Run(fmt.Sprintf("%s/%d/%s/%s/%s", rule.flag, value, lookup, verb, source), func(t *testing.T) {
							t.Parallel()

							routes := map[string]string{"GET /v1/clusters/c1": validCluster, "POST /v1/clusters": `{"id":"op","done":true}`, "PATCH /v1/clusters/c1": `{"id":"op","done":true}`}
							if lookup == "found" {
								routes["GET /v1/specifications/s"] = validSpec
							}

							if lookup == "failed" {
								routes["GET /v1/specifications/s"] = `{"message":"spec unavailable"}`
							}

							f := newFake(routes)
							if lookup == "failed" {
								f.statuses = map[string]int{"GET /v1/specifications/s": 500}
							}

							args := strings.Fields(createArgs)
							if verb == "update" {
								args = []string{"update", "c1"}
							}

							if source == "flag" {
								args = append(args, "--"+rule.flag, strconv.Itoa(value))
							} else {
								args = append(args, "--body", fmt.Sprintf(`{"%s":%d}`, rule.field, value))
							}

							out, msg, err := runWith(t, f, true, nil, args...)

							want := ""
							if lookup == "missing" {
								want = "no such specification: s"
							}

							if lookup == "found" && value < rule.min {
								want = fmt.Sprintf("%s %d, specification minimum is %d", rule.label, value, rule.min)
							}

							if lookup == "found" && rule.max != 0 && value > rule.max {
								want = fmt.Sprintf("%s %d, specification maximum is %d", rule.label, value, rule.max)
							}

							method := "POST /v1/clusters"
							if verb == "update" {
								method = "PATCH /v1/clusters/c1"
							}

							if want != "" {
								require.ErrorContains(t, err, want)
								assert.NotContains(t, f.requests(), method)
								assert.Empty(t, out)
							} else {
								require.NoError(t, err)
								assert.Contains(t, f.requests(), method)
								assert.Contains(t, out, `"op"`)
								assert.Contains(t, f.body(method), fmt.Sprintf(`"%s":%d`, rule.field, value))
							}

							assert.Contains(t, f.requests(), "GET /v1/specifications/s")

							if lookup == "failed" {
								assert.Contains(t, msg, "sending the request for API validation")
							} else {
								assert.Empty(t, msg)
							}
						})
					}
				}
			}
		}
	}
}

// Старый subnet_id запрещён и после mergeBody; отсутствие ограничений на кратность подсетей сохраняется.
func TestClusterSubnetRequest(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"create", "update"} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy=%t", verb, legacy), func(t *testing.T) {
				t.Parallel()

				f := newFake(map[string]string{"GET /v1/specifications/s": validSpec, "GET /v1/clusters/c1": validCluster, "POST /v1/clusters": `{"id":"op"}`, "PATCH /v1/clusters/c1": `{"id":"op"}`})
				args := strings.Fields(createArgs)

				method := "POST /v1/clusters"
				if verb == "update" {
					args = []string{"update", "c1"}
					method = "PATCH /v1/clusters/c1"
				}

				args = append(args, "--data-subnet-id", "a", "--data-subnet-id", "b", "--master-subnet-id", "c", "--master-subnet-id", "d")
				if legacy {
					args = append(args, "--body", `{"subnet_id":"legacy"}`)
				}

				_, err := run(t, f, true, args...)
				if legacy {
					require.ErrorContains(t, err, "subnet_id is deprecated")
					assert.NotContains(t, f.requests(), method)

					return
				}

				require.NoError(t, err)

				body := f.body(method)
				assert.NotContains(t, body, `"subnet_id"`)

				if verb == "create" {
					assert.Contains(t, body, `"data_nodes_subnet_ids":["a","b"]`)
					assert.Contains(t, body, `"master_nodes_subnet_ids":["c","d"]`)
				} else {
					assert.Contains(t, body, `"data_nodes_subnet_ids":{"value":["a","b"]}`)
					assert.Contains(t, body, `"master_nodes_subnet_ids":{"value":["c","d"]}`)
				}
			})
		}
	}
}

// При смене только спецификации проверяются и сохранённые размеры; расписание не требует справочника.
func TestUpdateSpecificationSelection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		args  []string
		calls []string
		want  string
	}{
		{"flag", []string{"update", "c1", "--spec", "s2"}, []string{"GET /v1/clusters/c1", "GET /v1/specifications/s2"}, "maximum is 2"},
		{"body", []string{"update", "c1", "--spec", "s", "--body", `{"specification_id":"s2"}`}, []string{"GET /v1/clusters/c1", "GET /v1/specifications/s2"}, "maximum is 2"},
		{"schedule", []string{"update", "c1", "--snapshots-enabled=false"}, []string{"PATCH /v1/clusters/c1"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"GET /v1/clusters/c1": validCluster, "GET /v1/specifications/s2": `{"max_data_nodes":2}`, "PATCH /v1/clusters/c1": `{"id":"op"}`})

			_, err := run(t, f, true, tc.args...)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
			} else {
				require.NoError(t, err)
				assert.Contains(t, f.body("PATCH /v1/clusters/c1"), `"enabled":false`)
			}

			assert.Equal(t, tc.calls, f.requests())
		})
	}
}
