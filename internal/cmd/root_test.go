package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// help и дополнение не читают конфиг (§8.7): кривой файл не ломает справку и Tab.
func TestHelpAndCompletionWithoutConfig(t *testing.T) {
	newFakeCloud(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	path := filepath.Join(home, ".config", "cloudru-dbaas", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("profiles:\n  default:\n    clikhouse_url: http://typo\n    key_secret: s\n"), 0o644))

	_, _, err := execute(NewDBaaSRoot(), "", "redis", "clusters", "list")
	require.Error(t, err, "обычная команда читает конфиг и падает на опечатке")

	for _, args := range [][]string{
		{"help"},
		{"help", "redis"},
		{"completion", "bash"},
		{"__complete", "redis", "cl"},
		{"__complete", "--profile", "typo", "redis", "cl"},
	} {
		out, errOut, err := execute(NewDBaaSRoot(), "", args...)
		require.NoError(t, err, args)
		assert.NotEmpty(t, out, args)
		assert.NotContains(t, errOut, "chmod", "без предупреждения о правах: %v", args)
	}
}
