package auth

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// FileStore хранит токены файлами token-<ключ>.json в Dir с правами 0600.
type FileStore struct {
	Dir string
}

// Load читает токен. Файл чужой (права шире 0600), ссылка или не обычный файл — как пустой кэш.
func (s FileStore) Load(key string) (Token, bool, error) {
	raw, ok := readPrivateFile(s.path(key))
	if !ok {
		return Token{}, false, nil
	}

	var d diskToken
	if err := json.Unmarshal(raw, &d); err != nil {
		return Token{}, false, i18n.Errorf("auth.err.cache", err)
	}

	return Token{AccessToken: d.Value, ExpiresAt: d.Expires}, true, nil
}

// Save пишет токен через временный файл и rename: os.WriteFile не меняет права уже
// существующего файла, а CreateTemp создаёт новый с 0600.
func (s FileStore) Save(key string, t Token) error {
	p := s.path(key)

	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return i18n.Errorf("auth.err.cache", err)
	}

	raw, err := json.Marshal(diskToken{Value: t.AccessToken, Expires: t.ExpiresAt})
	if err != nil {
		return i18n.Errorf("auth.err.cache", err)
	}

	f, err := os.CreateTemp(filepath.Dir(p), ".token-*")
	if err != nil {
		return i18n.Errorf("auth.err.cache", err)
	}

	_, werr := f.Write(raw)
	cerr := f.Close()

	err = errors.Join(werr, cerr)
	if err == nil {
		err = os.Rename(f.Name(), p)
	}

	if err == nil {
		return nil
	}

	_ = os.Remove(f.Name())

	return i18n.Errorf("auth.err.cache_write", p, err)
}

func (s FileStore) path(key string) string { return filepath.Join(s.Dir, "token-"+key+".json") }

// diskToken — формат файла кэша.
type diskToken struct {
	Value   string    `json:"value"`
	Expires time.Time `json:"expires"`
}

// readPrivateFile читает кэш, только если это обычный файл (не ссылка) и
// никому, кроме владельца, не открыт: токен в чужом файле мог утечь, а
// Save заменит его новым с 0600. Права проверяются у уже открытого
// дескриптора, а SameFile с Lstat ловит подмену между проверкой и открытием —
// иначе в окне между Stat и ReadFile файл можно было заменить ссылкой.
// На Windows права POSIX не значат ничего; каталог кэша в %LocalAppData%
// закрыт ACL профиля.
func readPrivateFile(p string) ([]byte, bool) {
	li, err := os.Lstat(p)
	if err != nil || !li.Mode().IsRegular() {
		return nil, false
	}

	f, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || !os.SameFile(li, fi) || (runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0) {
		return nil, false
	}

	raw, err := io.ReadAll(io.LimitReader(f, maxResponseSize))
	if err != nil {
		return nil, false
	}

	return raw, true
}
