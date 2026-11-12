package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rediscmd "github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis"
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

// Находка 12.11: создание занимает около 7050 с, прежних 30 мин не хватает.
// Общий флаг должен передать ожиданию 2 ч 30 мин, явный --timeout сохраняется.
func TestRootCreateWaitTimeout(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		want  time.Duration
	}{
		{"по умолчанию", nil, 150 * time.Minute},
		{"явный предел", []string{"--timeout", "3h"}, 3 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeCloud(t)
			f.on("POST /v1/clusters", jsonReply(`{"id":"op1","done":false}`))
			f.on("GET /v1/operations/op1", jsonReply(`{"id":"op1","done":true,"resource_id":"c1"}`))
			f.on("GET /v1/clusters/c1", jsonReply(`{"id":"c1","status":"READY"}`))

			a := newApp()
			root := &cobra.Command{Use: "cloudru-dbaas", PersistentPreRunE: func(cmd *cobra.Command, _ []string) error { return a.init(cmd) }}
			a.commonFlags(root)
			root.AddCommand(rediscmd.NewCmd(a.st, ""))

			out, _, err := execute(root, "", append([]string{"redis", "clusters", "create", "--name", "n", "--version-id", "v", "--spec", "s", "--wait"}, tc.flags...)...)
			require.NoError(t, err)
			assert.Equal(t, tc.want, a.st.Timeout)
			assert.Contains(t, out, "READY")
			assert.Equal(t, []string{"POST /v1/clusters", "GET /v1/operations/op1", "GET /v1/clusters/c1"}, f.calls)
		})
	}
}
