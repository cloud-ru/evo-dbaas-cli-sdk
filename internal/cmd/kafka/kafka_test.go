package kafka

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func TestNewCmdRegistersSevenResources(t *testing.T) {
	t.Parallel()

	cmd := NewCmd(&state.State{}, "https://kafka.example")

	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}

	assert.ElementsMatch(t, []string{"clusters", "operations", "roles", "specs", "topics", "users", "versions"}, names)
	assert.Len(t, cmd.Commands(), 7)
}

func TestAPIURLUsesConfiguredDefaultAndWritesFlagToState(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		defaultURL string
		args       []string
		want       string
	}{
		{"адрес из аргумента", "https://configured.example", nil, "https://configured.example"},
		{"флаг заменяет адрес", "https://configured.example", []string{"--api-url", "https://override.example"}, "https://override.example"},
		{"пустой адрес по умолчанию", "", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := &state.State{KafkaURL: "старое значение"}
			cmd := NewCmd(s, tc.defaultURL)
			assert.Equal(t, tc.defaultURL, s.KafkaURL)

			flag := cmd.PersistentFlags().Lookup("api-url")
			require.NotNil(t, flag)
			assert.Equal(t, tc.defaultURL, flag.DefValue)
			// Справка проверяет флаг без создания клиента API.
			cmd.SetArgs(append(append([]string(nil), tc.args...), "versions", "list", "--help"))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			assert.Equal(t, tc.want, s.KafkaURL)
		})
	}
}
