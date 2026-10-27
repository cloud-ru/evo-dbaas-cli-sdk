package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// countingFetch выдаёт tok-N и считает вызовы.
func countingFetch(calls *int, ttl time.Duration) func(context.Context) (Token, error) {
	return func(context.Context) (Token, error) {
		*calls++

		return Token{AccessToken: "fresh", ExpiresAt: t0.Add(ttl)}, nil
	}
}

func TestCachedSourceToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stored    Token
		found     bool
		loadErr   error
		wantToken string
		wantFetch int
	}{
		{"в кэше действующий", Token{"cached", t0.Add(30 * time.Minute)}, true, nil, "cached", 0},
		{"до конца меньше минуты — свежий", Token{"cached", t0.Add(59 * time.Second)}, true, nil, "fresh", 1},
		{"истёк — свежий", Token{"cached", t0.Add(-time.Minute)}, true, nil, "fresh", 1},
		{"кэша нет", Token{}, false, nil, "fresh", 1},
		{"кэш не прочитался — свежий", Token{}, false, errors.New("broken json"), "fresh", 1},
		{"пустой токен в кэше — свежий", Token{"", t0.Add(time.Hour)}, true, nil, "fresh", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newMockStore(t)
			store.EXPECT().Load("k1").Return(tt.stored, tt.found, tt.loadErr).Once()

			if tt.wantFetch > 0 {
				store.EXPECT().Save("k1", Token{"fresh", t0.Add(time.Hour)}).Return(nil).Once()
			}

			calls := 0
			s := &CachedSource{Fetch: countingFetch(&calls, time.Hour), Store: store, Key: "k1", Now: func() time.Time { return t0 }}

			got, err := s.Token(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, got)
			assert.Equal(t, tt.wantFetch, calls)
		})
	}
}

func TestCachedSourceRefreshBypassesStore(t *testing.T) {
	t.Parallel()

	store := newMockStore(t)
	store.EXPECT().Save("k1", mock.Anything).Return(errors.New("disk full")).Once()

	calls := 0
	s := &CachedSource{Fetch: countingFetch(&calls, time.Hour), Store: store, Key: "k1"}

	got, err := s.Refresh(t.Context())
	require.NoError(t, err, "не сохранили — не ошибка: токен всё равно есть")
	assert.Equal(t, "fresh", got)
	assert.Equal(t, 1, calls)
}

func TestCachedSourceFetchError(t *testing.T) {
	t.Parallel()

	s := &CachedSource{Fetch: func(context.Context) (Token, error) { return Token{}, ErrNoKey }}

	_, err := s.Token(t.Context())
	require.ErrorIs(t, err, ErrNoKey)
}

func TestCacheKey(t *testing.T) {
	t.Parallel()

	base := CacheKey("kid", "sec", "http://iam")

	assert.Len(t, base, 16)
	assert.Equal(t, base, CacheKey("kid", "sec", "http://iam"))
	assert.NotEqual(t, base, CacheKey("kid", "sec-2", "http://iam"), "секрет сменили — старый токен не годится")
	assert.NotEqual(t, base, CacheKey("kid", "sec", "http://iam/"), "другой адрес IAM — тоже")
	assert.NotContains(t, base, "sec")
}
