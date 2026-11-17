package spec

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// Лишний аргумент не должен молча превращать get/list в запрос к другому ресурсу.
// Сетка и TestAPICoverage передают только допустимые аргументы, cloud не разбирает CLI.
func TestSpecificationsRejectArguments(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"list extra", []string{"list", "extra"}, "unknown command"},
		{"get missing", []string{"get"}, "accepts 1 arg(s), received 0"},
		{"get extra", []string{"get", "s", "extra"}, "accepts 1 arg(s), received 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			called := false
			cmd := NewCmd(&state.State{OpenSearch: func() (*cloud.OpenSearch, error) { called = true; return nil, errors.New("unexpected initialization") }})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.ExecuteContext(t.Context())
			require.ErrorContains(t, err, tc.want)
			assert.False(t, called)
		})
	}
}

// Живой вызов 16.11: без версии API отвечает 400 «version is required». Ловит уход запроса без
// --version/--cluster-id; таблица — что текстовый вывод не сырой JSON.
func TestSpecList(t *testing.T) {
	t.Parallel()

	called := false
	cmd := NewCmd(&state.State{OpenSearch: func() (*cloud.OpenSearch, error) { called = true; return nil, errors.New("unexpected initialization") }})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"list"})
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "--version")
	assert.False(t, called)

	var buf bytes.Buffer
	require.NoError(t, printSpecs(&buf, []byte(`{"specifications":[{"id":"7e40","name":"4vCPU/16GB RAM (Business)","data_nodes_cpu":4,"data_nodes_memory":16,"min_data_nodes":3,"max_data_nodes":6,"min_master_nodes":0,"max_master_nodes":0,"min_data_nodes_storage_gigs":40}]}`)))
	assert.Contains(t, buf.String(), "DATA_NODES")
	assert.Contains(t, buf.String(), "3-6")
	assert.NotContains(t, buf.String(), "{")
}

// Живой прогон 17.11: cluster_id вместе с version/zone API отклоняет 400 — CLI отказывает до запроса.
func TestSpecListClusterExclusive(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"--version", "--zone"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()

			called := false
			cmd := NewCmd(&state.State{OpenSearch: func() (*cloud.OpenSearch, error) { called = true; return nil, errors.New("unexpected initialization") }})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"list", "--cluster-id", "c1", flag, "x"})
			err := cmd.ExecuteContext(t.Context())
			require.ErrorContains(t, err, "cluster-id")
			assert.False(t, called)
		})
	}
}
