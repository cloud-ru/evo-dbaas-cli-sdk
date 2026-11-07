package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResizeControllerPair(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		flags []string
		body  string
	}{
		{"без контроллеров", []string{"--spec", "s2"}, `{"cluster_id":"c1","specification_id":"s2"}`},
		{"пара", []string{"--controller-spec", "cs", "--controller-storage-gib", "10"}, `{"cluster_id":"c1","controller_spec":{"specification_id":"cs","storage_gib":10}}`},
		{"body перекрывает", []string{"--controller-spec", "cs", "--controller-storage-gib", "10", "--body", `{"controller_spec":{"specification_id":"cs2","storage_gib":20}}`}, `{"cluster_id":"c1","controller_spec":{"specification_id":"cs2","storage_gib":20}}`},
		{"только спецификация", []string{"--controller-spec", "cs"}, ""},
		{"только диск", []string{"--controller-storage-gib", "10"}, ""},
		{"нулевой диск", []string{"--controller-spec", "cs", "--controller-storage-gib", "0"}, ""},
		{"отрицательный диск", []string{"--controller-spec", "cs", "--controller-storage-gib", "-1"}, ""},
		{"пустая спецификация", []string{"--controller-spec", "", "--controller-storage-gib", "10"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{"PATCH /v1/clusters/c1": `{"id":"op1","done":false}`})

			_, err := run(t, f, false, append([]string{"resize", "c1"}, tc.flags...)...)
			if tc.body == "" {
				require.ErrorContains(t, err, "--controller-spec and --controller-storage-gib must be specified together")
				assert.Empty(t, f.requests())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, []string{"PATCH /v1/clusters/c1"}, f.requests())
			assert.JSONEq(t, tc.body, f.body("PATCH /v1/clusters/c1"))
		})
	}
}
