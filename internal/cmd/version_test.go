package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionMatchesFlag(t *testing.T) {
	previous := Version
	Version = "v9.9.9-test"
	t.Cleanup(func() { Version = previous })

	want, stderr, err := execute(NewDBaaSRoot(), "", "--version")
	require.NoError(t, err)
	require.Equal(t, "cloudru-dbaas version v9.9.9-test\n", want)
	assert.Empty(t, stderr)

	got, stderr, err := execute(NewDBaaSRoot(), "", "version")
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Empty(t, stderr)
}

func TestVersionJSON(t *testing.T) {
	previous := Version
	Version = "v9.9.9-test"
	t.Cleanup(func() { Version = previous })

	stdout, stderr, err := execute(NewDBaaSRoot(), "", "version", "--json")
	require.NoError(t, err)
	assert.Empty(t, stderr)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, map[string]any{"version": "v9.9.9-test"}, got)
}

func TestVersionWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, name := range []string{"CLOUDRU_KEY_ID", "CLOUDRU_KEY_SECRET", "CLOUDRU_CONFIG", "CLOUDRU_PROFILE"} {
		t.Setenv(name, "")
	}

	stdout, stderr, err := execute(NewDBaaSRoot(), "", "version")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))
	assert.Equal(t, "cloudru-dbaas version "+Version+"\n", stdout)
	assert.Empty(t, stderr)

	// Инициализация клиентов ленивая: отсутствие ключа само по себе не доказывает обход init.
	stdout, stderr, err = execute(NewDBaaSRoot(), "", "version", "--profile", "missing")
	require.NoError(t, err)
	assert.Equal(t, "cloudru-dbaas version "+Version+"\n", stdout)
	assert.Empty(t, stderr)
}

func TestVersionRejectsArgs(t *testing.T) {
	stdout, stderr, err := execute(NewDBaaSRoot(), "", "version", "extra")
	require.ErrorContains(t, err, `unknown command "extra" for "cloudru-dbaas version"`)
	assert.Equal(t, 1, exitCode(err))
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
