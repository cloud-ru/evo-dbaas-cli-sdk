package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Запас до истечения: токен, которому осталось меньше минуты, не используем.
const refreshMargin = time.Minute

// Store — где хранить токен между запусками. Ошибка Load — как пустой кэш:
// токен всегда можно получить заново.
type Store interface {
	Load(key string) (Token, bool, error)
	Save(key string, t Token) error
}

// CachedSource отдаёт токен из Store, пока он действует, иначе берёт свежий через Fetch
// (обычно IAMSource.Fetch).
type CachedSource struct {
	Fetch func(ctx context.Context) (Token, error)
	Store Store // nil — без кэша
	Key   string
	Now   func() time.Time
}

// CacheKey — ключ кэша по KeyID, секрету и адресу IAM: смена любого из них не подхватит
// старый токен. Сам секрет в ключ не попадает.
func CacheKey(keyID, secret, iamURL string) string {
	h := sha256.Sum256([]byte(keyID + "\x00" + secret + "\x00" + iamURL))

	return hex.EncodeToString(h[:8])
}

// Token возвращает действующий токен: из хранилища или свежий.
func (s *CachedSource) Token(ctx context.Context) (string, error) {
	if s.Store != nil {
		if t, ok, err := s.Store.Load(s.Key); err == nil && ok && t.AccessToken != "" &&
			s.now().Add(refreshMargin).Before(t.ExpiresAt) {
			return t.AccessToken, nil
		}
	}

	return s.Refresh(ctx)
}

// Refresh берёт токен у IAM мимо хранилища и перезаписывает его. Нужен, когда API
// отверг токен из кэша раньше срока (например, токен отозван при живом ключе).
func (s *CachedSource) Refresh(ctx context.Context) (string, error) {
	t, err := s.Fetch(ctx)
	if err != nil {
		return "", err
	}

	if s.Store != nil {
		_ = s.Store.Save(s.Key, t) // не сохранили — следующий запуск возьмёт токен заново
	}

	return t.AccessToken, nil
}

func (s *CachedSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}
