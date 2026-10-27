package i18n

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Язык — общее состояние пакета: тесты здесь не параллельные и возвращают en.
func TestSetLanguage(t *testing.T) {
	t.Cleanup(func() { SetLanguage("") })

	for _, tc := range []struct{ posix, want string }{
		{"", "Availability zones (for specs list --zone)"},
		{"C.UTF-8", "Availability zones (for specs list --zone)"},
		{"ru_RU.UTF-8", "Зоны доступности (для specs list --zone)"},
		{"RU", "Зоны доступности (для specs list --zone)"},
		{"en_US.UTF-8", "Availability zones (for specs list --zone)"},
	} {
		SetLanguage(tc.posix)
		assert.Equal(t, tc.want, T("zones.short"), tc.posix)
	}
}

func TestTFormatsArgs(t *testing.T) {
	assert.Equal(t, "cannot parse the zone list: bad", T("zones.err.parse", "bad"))
}

func TestTMissingIDReturnsID(t *testing.T) {
	assert.Equal(t, "no.such.id", T("no.such.id"))
	assert.Equal(t, "no.such.id", T("no.such.id", "arg"), "без %!(EXTRA …)")
}

func TestErrorfMissingIDKeepsWrapped(t *testing.T) {
	base := errors.New("API 403")
	err := Errorf("no.such.id", "x", base)

	require.ErrorIs(t, err, base)
	require.EqualError(t, err, "no.such.id: API 403")
	assert.EqualError(t, Errorf("no.such.id"), "no.such.id")
}

// Sentinel создаётся до выбора языка, а текст отдаёт уже на выбранном.
func TestSentinelTranslatesLazily(t *testing.T) {
	t.Cleanup(func() { SetLanguage("") })

	err := Sentinel("zones.short")

	SetLanguage("ru_RU.UTF-8")
	require.EqualError(t, err, "Зоны доступности (для specs list --zone)")
	require.ErrorIs(t, fmt.Errorf("x: %w", err), Sentinel("zones.short"))
}

func TestErrorfWraps(t *testing.T) {
	base := errors.New("API 403")
	err := Errorf("zones.err.list", base)

	require.ErrorIs(t, err, base)
	assert.EqualError(t, err, "zones: API 403")
}
