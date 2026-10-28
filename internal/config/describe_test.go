package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Источник каждого ключа — тот, что взял бы Load: env > профиль файла > умолчание; незаданный — none.
func TestDescribeSources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeConfig(t, dir, "profile: stand\nprofiles:\n  stand:\n    key_id: kid-file\n    project_id: p-file\n    allow_http: true\n", 0o600)

	r, err := describe(Options{}, environ("XDG_CONFIG_HOME="+dir, "CLOUDRU_PROJECT_ID=p-env"))
	require.NoError(t, err)

	assert.Equal(t, path, r.File)
	assert.True(t, r.FileExists)
	assert.Equal(t, "stand", r.Profile)
	assert.Equal(t, "file", r.ProfileSource)

	got := map[string]Setting{}
	for _, s := range r.Settings {
		got[s.Key] = s
	}

	assert.Len(t, r.Settings, len(keys), "все ключи, и незаданные")
	assert.Equal(t, Setting{"key_id", "kid-file", "file stand"}, got["key_id"])
	assert.Equal(t, Setting{"key_secret", "", "none"}, got["key_secret"])
	assert.Equal(t, Setting{"project_id", "p-env", "env CLOUDRU_PROJECT_ID"}, got["project_id"])
	assert.Equal(t, Setting{"redis_url", "https://redis.api.cloud.ru", "default"}, got["redis_url"])
	assert.Equal(t, Setting{"allow_http", "true", "file stand"}, got["allow_http"])
	assert.Equal(t, "key_id", r.Settings[0].Key, "порядок полей Config")
}

func TestDescribeProfileSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfig(t, dir, "profile: a\nprofiles:\n  a: {}\n  b: {}\n  default: {}\n", 0o600)

	for _, tc := range []struct {
		name, flag, env, want, from string
	}{
		{"флаг", "b", "a", "b", "flag"},
		{"env", "", "b", "b", "env CLOUDRU_PROFILE"},
		{"строка profile в файле", "", "", "a", "file"},
	} {
		r, err := describe(Options{Profile: tc.flag}, environ("XDG_CONFIG_HOME="+dir, "CLOUDRU_PROFILE="+tc.env))
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.want, r.Profile, tc.name)
		assert.Equal(t, tc.from, r.ProfileSource, tc.name)
	}

	r, err := describe(Options{}, environ("XDG_CONFIG_HOME="+t.TempDir()))
	require.NoError(t, err)
	assert.Equal(t, "default", r.Profile)
	assert.Equal(t, "default", r.ProfileSource)
	assert.False(t, r.FileExists, "файла нет — не ошибка")

	s, err := r.Get("allow_http")
	require.NoError(t, err)
	assert.Equal(t, Setting{"allow_http", "false", "default"}, s, "у allow_http есть умолчание, не none")
}

func TestDescribeFileError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfig(t, dir, "profiles:\n  default:\n    clikhouse_url: http://typo\n", 0o600)

	_, err := describe(Options{}, environ("XDG_CONFIG_HOME="+dir))
	require.ErrorContains(t, err, "profiles.default.clikhouse_url")
}

func TestDescribeMasksSecret(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ secret, want string }{
		{"abcdefghijklmnopWXYZ", "****WXYZ"},
		{"abcdefghijklmnoP", "****mnoP"},
		{"abcdefghijklmno", "****"},
		{"s", "****"},
	} {
		r, err := describe(Options{}, environ("CLOUDRU_KEY_SECRET="+tc.secret))
		require.NoError(t, err)

		s, err := r.Get("key_secret")
		require.NoError(t, err)
		assert.Equal(t, tc.want, s.Value, tc.secret)
		assert.Equal(t, "env CLOUDRU_KEY_SECRET", s.Source)
	}
}

func TestReportOverrideAndGet(t *testing.T) {
	t.Parallel()

	r, err := describe(Options{}, environ("CLOUDRU_PROJECT_ID=p-env"))
	require.NoError(t, err)

	r.Override("project_id", "p-flag")

	s, err := r.Get("project_id")
	require.NoError(t, err)
	assert.Equal(t, Setting{"project_id", "p-flag", "flag"}, s)

	_, err = r.Get("clikhouse_url")
	require.ErrorContains(t, err, `"clikhouse_url"`)
	require.ErrorContains(t, err, "allow_http", "список ключей в ошибке")
}
