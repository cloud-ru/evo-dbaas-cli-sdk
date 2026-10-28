package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFile(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(raw)
}

// Файла и каталога нет — set создаёт их с правами 0700 и 0600, значение читает Load.
func TestSetCreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	env := environ("XDG_CONFIG_HOME=" + dir)

	ch, err := set(Options{}, env, "key_id", "0123")
	require.NoError(t, err)

	path := filepath.Join(dir, "cloudru-dbaas", "config.yaml")
	assert.Equal(t, Change{Path: path, Profile: "default"}, ch)

	c, err := load(Options{}, env)
	require.NoError(t, err)
	assert.Equal(t, "0123", c.KeyID, "цифры — строка, не число: иначе файл не пройдёт строгую проверку")

	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{filepath.Dir(path): 0o700, path: 0o600} {
			fi, err := os.Stat(p)
			require.NoError(t, err)
			assert.Equal(t, want, fi.Mode().Perm(), p)
		}
	}
}

// Профиль для записи — тот же порядок, что при чтении: флаг > CLOUDRU_PROFILE > profile: > default.
func TestSetProfileChoice(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, flag, env, want string
	}{
		{"флаг сильнее env", "a", "b", "a"},
		{"env сильнее файла", "", "b", "b"},
		{"profile: в файле", "", "", "c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeConfig(t, dir, "profile: c\nprofiles:\n  c: {}\n", 0o600)

			ch, err := set(Options{Profile: tc.flag}, environ("XDG_CONFIG_HOME="+dir, "CLOUDRU_PROFILE="+tc.env), "project_id", "p")
			require.NoError(t, err)
			assert.Equal(t, tc.want, ch.Profile)

			c, err := load(Options{Profile: tc.want}, environ("XDG_CONFIG_HOME="+dir))
			require.NoError(t, err)
			assert.Equal(t, "p", c.ProjectID)
		})
	}
}

// Правка не теряет комментарии и чужие профили, пустой профиль становится словарём,
// права шире 0600 сужаются: файл заменяется новым.
func TestSetKeepsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeConfig(t, dir, "# мой конфиг\nprofiles:\n  default:\n    key_id: a # ключ\n  empty:\n", 0o644)

	_, err := set(Options{Profile: "empty"}, environ("XDG_CONFIG_HOME="+dir), "allow_http", "true")
	require.NoError(t, err)
	_, err = set(Options{}, environ("XDG_CONFIG_HOME="+dir), "key_id", "b")
	require.NoError(t, err)

	assert.Equal(t, "# мой конфиг\nprofiles:\n  default:\n    key_id: b # ключ\n  empty:\n    allow_http: true\n", readFile(t, path))

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}
}

func TestSetCommentOnlyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeConfig(t, dir, "# заполню потом\n", 0o600)

	_, err := set(Options{}, environ("XDG_CONFIG_HOME="+dir), "key_id", "k")
	require.NoError(t, err)
	assert.Equal(t, "# заполню потом\nprofiles:\n  default:\n    key_id: k\n", readFile(t, path))
}

func TestSetRejects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, file, key, value, want string
	}{
		{"неизвестный ключ", "", "clikhouse_url", "x", `unknown key "clikhouse_url"`},
		{"allow_http не true/false", "", "allow_http", "yes", `want true or false, got "yes"`},
		{"кривой файл не переписывает", "profiles:\n  default:\n    clikhouse_url: x\n", "key_id", "k", "unknown key profiles.default.clikhouse_url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "cloudru-dbaas", "config.yaml")

			if tc.file != "" {
				writeConfig(t, dir, tc.file, 0o600)
			}

			_, err := set(Options{}, environ("XDG_CONFIG_HOME="+dir), tc.key, tc.value)
			require.ErrorContains(t, err, tc.want)

			if tc.file == "" {
				assert.NoFileExists(t, path)
			} else {
				assert.Equal(t, tc.file, readFile(t, path))
			}
		})
	}
}

// Переменная окружения перекрывает записанное — set говорит какая, иначе правка «не работает» молча.
func TestSetShadowedByEnv(t *testing.T) {
	t.Parallel()

	ch, err := set(Options{}, environ("XDG_CONFIG_HOME="+t.TempDir(), "CLOUDRU_REDIS_API_URL=http://env", "CLOUDRU_PROJECT_ID="), "redis_url", "http://file")
	require.NoError(t, err)
	assert.Equal(t, "CLOUDRU_REDIS_API_URL", ch.ShadowedBy)

	ch, err = set(Options{}, environ("XDG_CONFIG_HOME="+t.TempDir(), "CLOUDRU_PROJECT_ID="), "project_id", "p")
	require.NoError(t, err)
	assert.Empty(t, ch.ShadowedBy, "пустая переменная не перекрывает")
}

func TestSetNoPath(t *testing.T) {
	t.Parallel()

	_, err := set(Options{}, environ(), "key_id", "k")
	require.ErrorContains(t, err, "unknown without HOME")

	path := filepath.Join(t.TempDir(), "sub", "c.yaml")
	ch, err := set(Options{Path: path}, environ(), "key_id", "k")
	require.NoError(t, err, "--config задаёт путь и без HOME")
	assert.Equal(t, path, ch.Path)
}

// Ссылка на конфиг (dotfiles) остаётся ссылкой: пишем в файл, куда она ведёт.
func TestSetFollowsSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" { // символьные ссылки там требуют прав; CI — Linux
		return
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles.yaml")
	require.NoError(t, os.WriteFile(target, []byte("profiles: {}\n"), 0o600))

	link := filepath.Join(dir, "cloudru-dbaas", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
	require.NoError(t, os.Symlink(target, link))

	_, err := set(Options{}, environ("XDG_CONFIG_HOME="+dir), "key_id", "k")
	require.NoError(t, err)

	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "ссылка не заменена файлом")
	assert.Contains(t, readFile(t, target), "key_id: k")
}

func TestUseProfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeConfig(t, dir, "profile: a\nprofiles:\n  a: {}\n  b: {project_id: p-b}\n", 0o600)
	env := environ("XDG_CONFIG_HOME=" + dir)

	_, err := useProfile(Options{}, env, "nope")
	require.ErrorContains(t, err, `no profile "nope" (available: a, b)`)

	got, err := useProfile(Options{}, env, "b")
	require.NoError(t, err)
	assert.Equal(t, path, got)

	c, err := load(Options{}, env)
	require.NoError(t, err)
	assert.Equal(t, "p-b", c.ProjectID)

	_, err = useProfile(Options{}, env, "default")
	require.NoError(t, err, "default есть всегда: без профиля из файла ничего не берётся")
}

// Имя профиля из цифр или «null» — строка: иначе set запишет файл, который Load отвергнет.
func TestSetProfileNameIsString(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"0123", "null", "true"} {
		dir := t.TempDir()
		env := environ("XDG_CONFIG_HOME=" + dir)

		_, err := set(Options{Profile: name}, env, "project_id", "p")
		require.NoError(t, err, name)
		_, err = useProfile(Options{}, env, name)
		require.NoError(t, err, name)

		c, err := load(Options{}, env)
		require.NoError(t, err, name)
		assert.Equal(t, "p", c.ProjectID, name)

		left, err := filepath.Glob(filepath.Join(dir, "cloudru-dbaas", ".config-*"))
		require.NoError(t, err)
		assert.Empty(t, left, "временный файл не остаётся")
	}
}

// Якоря, второй документ, битая ссылка: перезапись потеряла бы данные — отказ, файл цел.
func TestSetRefusesManualFiles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, file, want string }{
		{"профиль-ссылка", "profiles:\n  default: &b {key_id: k}\n  stand: *b\n", "edit it by hand"},
		{"слияние", "profiles:\n  default: &b {key_id: k}\n  stand:\n    <<: *b\n", "edit it by hand"},
		{"второй документ", "profiles: {}\n---\nprofiles: {}\n", "edit it by hand"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := writeConfig(t, dir, tc.file, 0o600)

			_, err := set(Options{Profile: "stand"}, environ("XDG_CONFIG_HOME="+dir), "redis_url", "http://x")
			require.ErrorContains(t, err, tc.want)
			assert.Equal(t, tc.file, readFile(t, path))
		})
	}

	if runtime.GOOS == "windows" { // символьные ссылки там требуют прав; CI — Linux
		return
	}

	dir := t.TempDir()
	link := filepath.Join(dir, "cloudru-dbaas", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
	require.NoError(t, os.Symlink(filepath.Join(dir, "нет", "c.yaml"), link))

	_, err := set(Options{}, environ("XDG_CONFIG_HOME="+dir), "key_id", "k")
	require.Error(t, err, "битая ссылка")

	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "ссылка не заменена файлом")
}
