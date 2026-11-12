package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update — синоним resize: в API операция ClusterService_Update, и resize меняет не только размер (ТГ 14572).
func TestResizeUpdateAlias(t *testing.T) {
	t.Parallel()

	f := newFake(map[string]string{"PATCH /v1/clusters/c1": `{"id":"op1","done":false}`})

	_, err := run(t, f, false, "update", "c1", "--spec", "s2")
	require.NoError(t, err)
	assert.Equal(t, []string{"PATCH /v1/clusters/c1"}, f.requests())
}
