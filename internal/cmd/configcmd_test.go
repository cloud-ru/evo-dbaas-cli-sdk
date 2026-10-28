package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configHome — HOME с файлом конфига по умолчанию; ключа и проекта в env нет.
func configHome(t *testing.T, body string) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_KEY_SECRET", "")
	t.Setenv("CLOUDRU_PROJECT_ID", "")
	t.Setenv("CLOUDRU_PROFILE", "")
	t.Setenv("CLOUDRU_CONFIG", "")

	path := filepath.Join(home, ".config", "cloudru-dbaas", "config.yaml")
	if body != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}

	return path
}

// config list и get работают без ключа (§8.7): показывают, что возьмёт команда и откуда.
func TestConfigListWithoutKey(t *testing.T) {
	path := configHome(t, "profiles:\n  stand:\n    key_id: kid\n    key_secret: abcdefghijklmnopWXYZ\n")

	out, _, err := execute(NewDBaaSRoot(), "", "config", "list", "--profile", "stand", "--project", "p-flag")
	require.NoError(t, err)

	assert.Contains(t, out, path)
	assert.Regexp(t, `\(profile\)\s+stand\s+flag`, out)
	assert.Regexp(t, `key_id\s+kid\s+file stand`, out)
	assert.Regexp(t, `key_secret\s+\*\*\*\*WXYZ\s+file stand`, out)
	assert.Regexp(t, `project_id\s+p-flag\s+flag`, out)
	assert.Regexp(t, `redis_url\s+https://redis.api.cloud.ru\s+default`, out)
	assert.NotContains(t, out, "abcdefghijklmnop", "секрет только маской")
}

func TestConfigListJSON(t *testing.T) {
	configHome(t, "")

	out, _, err := execute(NewDBaaSRoot(), "", "config", "list", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"file_exists":false`)
	assert.Contains(t, out, `{"key":"key_id","value":"","source":"none"}`)
}

func TestConfigListNoFileNoHome(t *testing.T) {
	path := configHome(t, "")

	out, _, err := execute(NewDBaaSRoot(), "", "config", "list")
	require.NoError(t, err)
	assert.Contains(t, out, path+" (does not exist)")

	t.Setenv("HOME", "")

	out, _, err = execute(NewDBaaSRoot(), "", "config", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "(path unknown without HOME)")
}

func TestConfigGet(t *testing.T) {
	configHome(t, "profiles:\n  default:\n    clickhouse_url: http://file\n")

	out, _, err := execute(NewDBaaSRoot(), "", "config", "get", "clickhouse_url")
	require.NoError(t, err)
	assert.Equal(t, "http://file\n", out)

	out, _, err = execute(NewDBaaSRoot(), "", "config", "get", "clickhouse_url", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"clickhouse_url","value":"http://file","source":"file default"}`, out)

	_, _, err = execute(NewDBaaSRoot(), "", "config", "get", "nope")
	require.ErrorContains(t, err, `unknown key "nope"`)
	assert.Equal(t, 1, exitCode(err))
}

// Кривой файл — та же ошибка, что у любой команды, код 1.
func TestConfigBadFile(t *testing.T) {
	configHome(t, "profiles:\n  default:\n    clikhouse_url: http://typo\n")

	for _, args := range [][]string{{"config", "list"}, {"config", "get", "key_id"}} {
		_, _, err := execute(NewDBaaSRoot(), "", args...)
		require.ErrorContains(t, err, "clikhouse_url", args)
		assert.Equal(t, 1, exitCode(err), args)
	}
}

// Явно пустой --project — пустой проект с источником flag, как в init: env не подставляется.
func TestConfigEmptyProjectFlag(t *testing.T) {
	configHome(t, "")
	t.Setenv("CLOUDRU_PROJECT_ID", "p-env")

	out, _, err := execute(NewDBaaSRoot(), "", "config", "get", "project_id", "--project=", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"project_id","value":"","source":"flag"}`, out)
}

// --config доходит до Describe; предупреждение о правах файла с секретом — в stderr.
func TestConfigFlagPathAndWarn(t *testing.T) {
	configHome(t, "")

	path := filepath.Join(t.TempDir(), "other.yaml")
	require.NoError(t, os.WriteFile(path, []byte("profiles:\n  default:\n    key_secret: s\n"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	out, errOut, err := execute(NewDBaaSRoot(), "", "config", "get", "key_secret", "--config", path, "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"key_secret","value":"****","source":"file default"}`, out)
	assert.Contains(t, errOut, "chmod 600 "+path)
}

// config set пишет без ключа; «-» — значение из stdin, секрет не попадает в аргументы и вывод.
func TestConfigSet(t *testing.T) {
	path := configHome(t, "")

	out, errOut, err := execute(NewDBaaSRoot(), "s3cret-value\r\n", "config", "set", "key_secret", "-", "--profile", "stand")
	require.NoError(t, err)
	assert.Empty(t, out)
	assert.Equal(t, "key_secret written to profile stand ("+path+")\n", errOut)
	assert.NotContains(t, errOut, "s3cret")

	out, _, err = execute(NewDBaaSRoot(), "", "config", "get", "key_secret", "--profile", "stand", "--json", "--config", path)
	require.NoError(t, err)
	assert.Contains(t, out, `"source":"file stand"`)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "key_secret: s3cret-value\n", "конец строки из stdin отрезан")

	_, _, err = execute(NewDBaaSRoot(), "", "config", "set", "key_secret", "-")
	require.ErrorContains(t, err, "stdin")
	assert.Equal(t, 1, exitCode(err))
}

func TestConfigSetShadowed(t *testing.T) {
	configHome(t, "")
	t.Setenv("CLOUDRU_PROJECT_ID", "p-env")

	_, errOut, err := execute(NewDBaaSRoot(), "", "config", "set", "project_id", "p-file")
	require.NoError(t, err)
	assert.Contains(t, errOut, "warning: CLOUDRU_PROJECT_ID is set and overrides this value")
}

// use-profile не смотрит на --profile: пишет именно названный профиль.
func TestConfigUseProfile(t *testing.T) {
	path := configHome(t, "profiles:\n  a: {}\n  b: {}\n")

	_, errOut, err := execute(NewDBaaSRoot(), "", "config", "use-profile", "b", "--profile", "a")
	require.NoError(t, err)
	assert.Equal(t, "default profile is now b ("+path+")\n", errOut)

	out, _, err := execute(NewDBaaSRoot(), "", "config", "list", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"profile":"b","profile_source":"file"`)

	_, _, err = execute(NewDBaaSRoot(), "", "config", "use-profile", "c")
	require.ErrorContains(t, err, `no profile "c" (available: a, b)`)
	assert.Equal(t, 1, exitCode(err))
}

func TestConfigUseProfileFlagPath(t *testing.T) {
	configHome(t, "")

	path := filepath.Join(t.TempDir(), "other.yaml")
	require.NoError(t, os.WriteFile(path, []byte("profiles:\n  stand: {}\n"), 0o600))

	_, _, err := execute(NewDBaaSRoot(), "", "config", "use-profile", "stand", "--config", path)
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "profile: stand")
}

func TestConfigUseProfileShadowed(t *testing.T) {
	configHome(t, "profiles:\n  a: {}\n")
	t.Setenv("CLOUDRU_PROFILE", "ghost")

	_, errOut, err := execute(NewDBaaSRoot(), "", "config", "use-profile", "a")
	require.NoError(t, err)
	assert.Contains(t, errOut, "warning: CLOUDRU_PROFILE is set and overrides this value")

	t.Setenv("CLOUDRU_PROFILE", "a")

	_, errOut, err = execute(NewDBaaSRoot(), "", "config", "use-profile", "a")
	require.NoError(t, err)
	assert.NotContains(t, errOut, "warning")
}
