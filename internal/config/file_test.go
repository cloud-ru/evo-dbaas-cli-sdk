package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfig кладёт body в dir/cloudru-dbaas/config.yaml — путь по умолчанию при XDG_CONFIG_HOME=dir.
func writeConfig(t *testing.T, dir, body string, mode os.FileMode) string {
	t.Helper()

	path := filepath.Join(dir, "cloudru-dbaas", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(body), mode))
	require.NoError(t, os.Chmod(path, mode))

	return path
}

func TestLoadFileUnderEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfig(t, dir, "profiles:\n  default:\n    project_id: p-file\n    clickhouse_url: http://file\n", 0o600)

	c, err := load(Options{}, environ("XDG_CONFIG_HOME="+dir, "CLOUDRU_PROJECT_ID=p-env"))
	require.NoError(t, err)

	assert.Equal(t, "p-env", c.ProjectID, "env перекрывает файл")
	assert.Equal(t, "http://file", c.ClickHouseURL, "файл перекрывает значение по умолчанию")
	assert.Equal(t, "https://redis.api.cloud.ru", c.RedisURL, "незаданное в профиле — по умолчанию")
}

func TestLoadProfileChoice(t *testing.T) {
	t.Parallel()

	const profiles = "profiles:\n  default: {project_id: p-default}\n  a: {project_id: p-a}\n  b: {project_id: p-b}\n  c: {project_id: p-c}\n"

	for _, tc := range []struct {
		name, file, flag, env, want string
	}{
		{"флаг сильнее env", "profile: c\n" + profiles, "a", "b", "p-a"},
		{"env сильнее файла", "profile: c\n" + profiles, "", "b", "p-b"},
		{"profile: в файле", "profile: c\n" + profiles, "", "", "p-c"},
		{"без выбора — default", profiles, "", "", "p-default"},
		{"профиль самодостаточен, default не наследуется", "profiles:\n  default: {project_id: p-default}\n  a: {key_id: k}\n", "a", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeConfig(t, dir, tc.file, 0o600)

			c, err := load(Options{Profile: tc.flag}, environ("XDG_CONFIG_HOME="+dir, "CLOUDRU_PROFILE="+tc.env))
			require.NoError(t, err)
			assert.Equal(t, tc.want, c.ProjectID)
		})
	}
}

func TestLoadNamedProfileMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeConfig(t, dir, "profile: gone\nprofiles:\n  stand: {}\n  prod: {}\n", 0o600)

	for _, o := range []Options{{Profile: "nope"}, {}} {
		_, err := load(o, environ("XDG_CONFIG_HOME="+dir))
		require.Error(t, err, o)
		assert.Contains(t, err.Error(), path)
		assert.Contains(t, err.Error(), "prod, stand", "список имеющихся")
	}

	_, err := load(Options{}, environ("HOME="+t.TempDir(), "CLOUDRU_PROFILE=stand"))
	require.Error(t, err, "профиль назван, а файла нет")

	writeConfig(t, dir, "profiles:\n  stand: {}\n", 0o600)

	_, err = load(Options{}, environ("XDG_CONFIG_HOME="+dir))
	require.NoError(t, err, "default может не быть")
}

func TestLoadStrictFile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		body, key string
	}{
		{"profiles:\n  default:\n    clikhouse_url: http://typo\n", "profiles.default.clikhouse_url"},
		{"profiles:\n  default:\n    CH_CLOUDRU_API_URL: http://old\n", "profiles.default.CH_CLOUDRU_API_URL"},
		{"current: stand\n", "current"},
		{"profiles:\n  default:\n    allow_http: да\n", "profiles.default.allow_http"},
		{"profiles:\n  default:\n    key_id: 123\n", "profiles.default.key_id"},
		{"profiles:\n  - default\n", "profiles"},
		{"profiles:\n  stand: http://x\n", "profiles.stand"},
		{"profile: [a]\n", "profile"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := writeConfig(t, dir, tc.body, 0o600)

			_, err := load(Options{}, environ("XDG_CONFIG_HOME="+dir))
			require.Error(t, err)
			assert.Contains(t, err.Error(), path)
			assert.Contains(t, err.Error(), tc.key)
		})
	}
}

func TestLoadFilePath(t *testing.T) {
	t.Parallel()

	home, xdg, other := t.TempDir(), t.TempDir(), t.TempDir()
	writeConfig(t, filepath.Join(home, ".config"), "profiles: {default: {project_id: p-home}}\n", 0o600)
	writeConfig(t, xdg, "profiles: {default: {project_id: p-xdg}}\n", 0o600)
	flagPath := writeConfig(t, other, "profiles: {default: {project_id: p-flag}}\n", 0o600)

	for _, tc := range []struct {
		name string
		o    Options
		env  []string
		want string
	}{
		{"~/.config без XDG", Options{}, []string{"HOME=" + home}, "p-home"},
		{"XDG_CONFIG_HOME", Options{}, []string{"HOME=" + home, "XDG_CONFIG_HOME=" + xdg}, "p-xdg"},
		{"CLOUDRU_CONFIG", Options{}, []string{"XDG_CONFIG_HOME=" + xdg, "CLOUDRU_CONFIG=" + flagPath}, "p-flag"},
		{"--config сильнее CLOUDRU_CONFIG", Options{Path: flagPath}, []string{"CLOUDRU_CONFIG=" + filepath.Join(xdg, "x.yaml")}, "p-flag"},
	} {
		if runtime.GOOS == "windows" {
			break
		}

		c, err := load(tc.o, environ(tc.env...))
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.want, c.ProjectID, tc.name)
	}

	_, err := load(Options{Path: filepath.Join(other, "missing.yaml")}, environ())
	require.Error(t, err, "явно заданного файла нет — ошибка")

	_, err = load(Options{}, environ("HOME="+home, "CLOUDRU_CONFIG="+filepath.Join(other, "missing.yaml")))
	require.Error(t, err, "файла из CLOUDRU_CONFIG нет — ошибка, а не тихий переход на файл по умолчанию")

	_, err = load(Options{}, environ("HOME="+t.TempDir()))
	require.NoError(t, err, "файла по умолчанию нет — не ошибка")
}

func TestLoadWarnsWideModeWithSecret(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		return
	}

	for _, tc := range []struct {
		name string
		body string
		mode os.FileMode
		warn bool
	}{
		{"секрет и 0644", "profiles: {default: {key_secret: s}}\n", 0o644, true},
		{"секрет и 0640 — читает группа", "profiles: {default: {key_secret: s}}\n", 0o640, true},
		{"секрет и 0604 — читают все", "profiles: {default: {key_secret: s}}\n", 0o604, true},
		{"секрет в невыбранном профиле", "profiles: {default: {key_id: k}, prod: {key_secret: s}}\n", 0o644, true},
		{"секрет и 0600", "profiles: {default: {key_secret: s}}\n", 0o600, false},
		{"без секрета и 0644", "profiles: {default: {key_id: k}}\n", 0o644, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := writeConfig(t, dir, tc.body, tc.mode)

			var w bytes.Buffer

			_, err := load(Options{Warn: &w}, environ("XDG_CONFIG_HOME="+dir))
			require.NoError(t, err)

			if tc.warn {
				assert.Contains(t, w.String(), "chmod 600 "+path)
			} else {
				assert.Empty(t, w.String())
			}
		})
	}
}

func TestLoadEmptyFileValueKeepsDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfig(t, dir, "profiles:\n  default:\n    vpc_url: \"\"\n    redis_url:\n    key_secret: \"\"\n", 0o644)

	var w bytes.Buffer

	c, err := load(Options{Warn: &w}, environ("XDG_CONFIG_HOME="+dir))
	require.NoError(t, err)
	assert.Equal(t, "https://vpc.api.cloud.ru", c.VPCURL, "пустая строка в файле — как не заданная, как у env")
	assert.Equal(t, "https://redis.api.cloud.ru", c.RedisURL, "ключ без значения — тоже")
	assert.Empty(t, w.String(), "пустой key_secret — не секрет")
}

func TestLoadNamedProfileWithoutFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	_, err := load(Options{Profile: "stand"}, environ("HOME="+home))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"stand"`)
	assert.Contains(t, err.Error(), filepath.Join(home, ".config", "cloudru-dbaas", "config.yaml"))
	assert.NotContains(t, err.Error(), "available", "файла нет — список профилей не к месту")

	_, err = load(Options{Profile: "stand"}, environ())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--config", "без HOME пути нет — подсказать флаг, а не пустой путь")
}

func TestLoadStrictFileStableError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfig(t, dir, "zz: 1\naa: 2\nprofiles:\n  b: {zz: 1}\n  a: {yy: 1, xx: 2}\n", 0o600)

	for range 20 {
		_, err := load(Options{}, environ("XDG_CONFIG_HOME="+dir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown key aa", "ключи по порядку — ошибка одна и та же")
	}

	writeConfig(t, dir, "profiles:\n  b: {zz: 1}\n  a: {yy: 1, xx: 2}\n", 0o600)

	for range 20 {
		_, err := load(Options{}, environ("XDG_CONFIG_HOME="+dir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "profiles.a.xx")
	}
}
