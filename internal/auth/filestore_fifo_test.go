package auth

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStoreFIFONotOpened(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("mkfifo"); err != nil {
		t.Skip("нет mkfifo")
	}

	s := FileStore{Dir: t.TempDir()}
	mk := exec.CommandContext(t.Context(), "mkfifo", "-m", "600", "token-k1.json")
	mk.Dir = s.Dir
	require.NoError(t, mk.Run())

	done := make(chan bool, 1)

	go func() {
		_, ok, _ := s.Load("k1")
		done <- ok
	}()

	select {
	case ok := <-done:
		assert.False(t, ok)
	case <-time.After(5 * time.Second):
		require.Fail(t, "open на FIFO в месте кэша повесил Load")
	}
}
