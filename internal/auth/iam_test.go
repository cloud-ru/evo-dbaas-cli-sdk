package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeIAM отвечает status и body и проверяет, что пришёл ключ kid/sec.
func fakeIAM(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string

		assert.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		assert.Equal(t, map[string]string{"keyId": "kid", "secret": "sec"}, in)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestIAMSourceFetch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	t.Run("токен и срок из expires_in", func(t *testing.T) {
		t.Parallel()

		srv := fakeIAM(t, http.StatusOK, `{"access_token":"tok-1","expires_in":7200}`)
		got, err := (&IAMSource{KeyID: "kid", Secret: "sec", URL: srv.URL, Now: clock, HTTP: srv.Client()}).Fetch(t.Context())

		require.NoError(t, err)
		assert.Equal(t, Token{AccessToken: "tok-1", ExpiresAt: now.Add(2 * time.Hour)}, got)
	})

	t.Run("expires_in 0 — запасной час", func(t *testing.T) {
		t.Parallel()

		srv := fakeIAM(t, http.StatusOK, `{"access_token":"tok","expires_in":0}`)
		got, err := (&IAMSource{KeyID: "kid", Secret: "sec", URL: srv.URL, Now: clock, HTTP: srv.Client()}).Fetch(t.Context())

		require.NoError(t, err)
		assert.Equal(t, now.Add(time.Hour), got.ExpiresAt)
	})

	t.Run("нет access_token", func(t *testing.T) {
		t.Parallel()

		srv := fakeIAM(t, http.StatusOK, `{"expires_in":3600}`)
		_, err := (&IAMSource{KeyID: "kid", Secret: "sec", URL: srv.URL, HTTP: srv.Client()}).Fetch(t.Context())

		require.ErrorIs(t, err, errNoToken)
	})

	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrKeyRejected},
		{http.StatusForbidden, ErrKeyForbidden},
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusBadGateway, nil},
	} {
		t.Run(fmt.Sprintf("IAM %d", tc.status), func(t *testing.T) {
			t.Parallel()

			srv := fakeIAM(t, tc.status, " {\"message\":\"bad key\"}\n")
			_, err := (&IAMSource{KeyID: "kid", Secret: "sec", URL: srv.URL, HTTP: srv.Client()}).Fetch(t.Context())

			var ie *IAMError

			require.ErrorAs(t, err, &ie)
			assert.Equal(t, &IAMError{Status: tc.status, Body: "bad key"}, ie)

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}
		})
	}
}

// redirectIAM отвечает редиректом code на сервер, который отмечает в leaked, что ключ ушёл по Location.
func redirectIAM(t *testing.T, code int, leaked *atomic.Bool) *httptest.Server {
	t.Helper()

	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Store(true) }))
	t.Cleanup(target.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, code)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestIAMSourceNoRedirect(t *testing.T) {
	t.Parallel()

	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			t.Parallel()

			var leaked atomic.Bool

			srv := redirectIAM(t, code, &leaked)

			// Свой клиент вызывающего тоже не должен ходить по редиректу.
			hc := srv.Client()
			_, err := (&IAMSource{KeyID: "kid", Secret: "sec", URL: srv.URL, HTTP: hc}).Fetch(t.Context())

			var ie *IAMError
			if assert.ErrorAs(t, err, &ie) {
				assert.Equal(t, code, ie.Status)
			}

			assert.Nil(t, hc.CheckRedirect, "клиент вызывающего не меняется")
			assert.False(t, leaked.Load(), "ключ ушёл по Location")
		})
	}
}

// Без t.Parallel: клиент по умолчанию ходит через http.DefaultTransport, а Close любого
// httptest.Server рвёт его соединения — параллельно с другими тестами пакета это флак
// «CloseIdleConnections called».
func TestIAMSourceNoRedirectDefaultClient(t *testing.T) {
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		var leaked atomic.Bool

		srv := redirectIAM(t, code, &leaked)
		_, err := (&IAMSource{KeyID: "kid", Secret: "sec", URL: srv.URL}).Fetch(t.Context())

		var ie *IAMError
		if assert.ErrorAs(t, err, &ie) {
			assert.Equal(t, code, ie.Status)
		}

		assert.False(t, leaked.Load(), "ключ ушёл по Location")
	}
}

func TestIAMSourceNoKey(t *testing.T) {
	t.Parallel()

	for _, s := range []*IAMSource{{}, {KeyID: "kid"}, {Secret: "sec"}} {
		_, err := s.Fetch(t.Context())
		require.ErrorIs(t, err, ErrNoKey)
	}
}

func TestIAMSourceEndpoint(t *testing.T) {
	t.Parallel()

	assert.Empty(t, (&IAMSource{}).Endpoint(), "адрес по умолчанию — в config, не в auth")
	assert.Equal(t, "http://x", (&IAMSource{URL: "http://x"}).Endpoint())
}

// Регрессия: сырое тело ответа IAM снова уходит в текст ошибки — и с ним keyId, если IAM начнёт его возвращать.
func TestIAMMessage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"message", `{"message":"bad key","keyId":"kid-123"}`, "bad key"},
		{"error_description раньше error", `{"error":"invalid_client","error_description":"unknown key"}`, "unknown key"},
		{"только error", `{"error":"invalid_client"}`, "invalid_client"},
		{"без текстового поля", `{"keyId":"kid-123","code":16}`, "Unauthorized"},
		{"не JSON", "keyId=kid-123 rejected", "Unauthorized"},
		{"пусто", "", "Unauthorized"},
		{"пустой message — следующее поле", `{"message":"","error":"invalid_client"}`, "invalid_client"},
		{"message из пробелов — следующее поле", `{"message":"   ","error":"invalid_client"}`, "invalid_client"},
		{"управляющие символы вырезаны", `{"message":"\u001b[2J\u001b]0;t\u0007bad\nerror: fake"}`, "[2J]0;tbaderror: fake"},
		{"длинный текст обрезан", `{"message":"` + strings.Repeat("я", maxIAMMessage+50) + `"}`, strings.Repeat("я", maxIAMMessage) + "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := iamMessage(http.StatusUnauthorized, []byte(tc.body))

			assert.Equal(t, tc.want, got)
			assert.NotContains(t, got, "kid-123")
		})
	}
}
