package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var tok = Token{AccessToken: "tok-1", ExpiresAt: time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)}

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()

	s := FileStore{Dir: filepath.Join(t.TempDir(), "nested")}

	_, ok, err := s.Load("k1")
	require.NoError(t, err)
	assert.False(t, ok, "пустой кэш")

	require.NoError(t, s.Save("k1", tok))

	got, ok, err := s.Load("k1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, tok, got)

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(s.path("k1"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}
}

func TestFileStoreFixesLoosePermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("права POSIX")
	}

	s := FileStore{Dir: t.TempDir()}
	require.NoError(t, s.Save("k1", tok))
	require.NoError(t, os.Chmod(s.path("k1"), 0o644))

	_, ok, err := s.Load("k1")
	require.NoError(t, err)
	assert.False(t, ok, "кэш 0644 прочитан")

	require.NoError(t, s.Save("k1", tok))

	fi, err := os.Stat(s.path("k1"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "права кэша после записи")
}

func TestFileStoreSymlinkNotRead(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("ссылки POSIX")
	}

	s := FileStore{Dir: t.TempDir()}
	other := FileStore{Dir: t.TempDir()}
	require.NoError(t, other.Save("k1", tok))
	// Кэш — ссылка на файл с валидным токеном и 0600: читать его нельзя.
	require.NoError(t, os.Symlink(other.path("k1"), s.path("k1")))

	_, ok, err := s.Load("k1")
	require.NoError(t, err)
	assert.False(t, ok, "кэш-ссылка прочитана")
}

func TestFileStoreBrokenJSON(t *testing.T) {
	t.Parallel()

	s := FileStore{Dir: t.TempDir()}
	require.NoError(t, os.WriteFile(s.path("k1"), []byte("{"), 0o600))

	_, ok, err := s.Load("k1")
	require.Error(t, err)
	assert.False(t, ok)
}
