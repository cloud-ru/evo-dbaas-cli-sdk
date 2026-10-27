package state

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/config"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

func TestCheckInsecureURL(t *testing.T) {
	for _, tc := range []struct {
		url   string
		allow string
		fail  bool
		warn  bool
	}{
		{"https://clickhouse.api.cloud.ru", "", false, false},
		{"", "", false, false},
		{"http://127.0.0.1:8080", "", false, false},
		{"http://localhost:9000/v1", "", false, false},
		{"http://[::1]:8080", "", false, false},
		{"http://api.example.com", "", true, false},
		{"http://10.0.0.5:8080", "", true, false},
		{"http://10.0.0.5:8080", "1", false, true},
		{"HTTP://api.example.com", "", true, false},
		{"http://127.0.0.2:8080", "", false, false},
		{"http://LOCALHOST:9000", "", false, false},
		{"http://localhost.:9000", "", false, false},
		{"http://localhost.example.com", "", true, false},
	} {
		var b bytes.Buffer

		err := checkInsecureURL(&b, tc.url, tc.allow == "1")
		if (err != nil) != tc.fail || (b.Len() > 0) != tc.warn {
			t.Errorf("%q allow=%q: err=%v warn=%q", tc.url, tc.allow, err, b.String())
		}
	}
}

func TestEmptyBodyHint(t *testing.T) {
	t.Parallel()

	for code, want := range map[int]string{
		400: "required parameter", 401: "after IAM refresh", 403: "check the project",
		404: "not found", 429: "try again in a minute", 500: "try again later", 502: "try again later",
	} {
		assert.Contains(t, emptyBodyHint(code), want, code)
	}

	assert.Equal(t, "empty response body", emptyBodyHint(418))
}

func TestConnectRefusesInsecureURL(t *testing.T) {
	t.Parallel()

	var errOut bytes.Buffer

	s := &State{Printer: &output.Printer{Out: io.Discard, Err: &errOut}}
	err := s.Connect(Conn{Config: config.Config{VPCURL: "http://vpc.example.com"}})
	require.ErrorContains(t, err, "CLOUDRU_ALLOW_HTTP")
	assert.Nil(t, s.VPC, "клиенты не собраны")

	s = &State{Printer: &output.Printer{Out: io.Discard, Err: &errOut}}
	require.NoError(t, s.Connect(Conn{Config: config.Config{VPCURL: "http://vpc.example.com", AllowHTTP: true}}))
	assert.Contains(t, errOut.String(), "no TLS")
	assert.NotNil(t, s.VPC)
}

// Connect передаёт транспорту подсказку для пустого тела и предел тела:
// каждую часть проверяют свои тесты, здесь — что они собраны вместе.
func TestConnectWiresTransport(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	t.Cleanup(iam.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/empty") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		_, _ = w.Write([]byte(`{"id":"` + strings.Repeat("x", 4<<10) + `"}`))
	}))
	t.Cleanup(api.Close)

	s := &State{Printer: &output.Printer{Out: io.Discard, Err: io.Discard}}
	require.NoError(t, s.Connect(Conn{
		Config:      config.Config{KeyID: "k", KeySecret: "s", IAMAuthURL: iam.URL, VPCURL: api.URL},
		RetryDelays: []time.Duration{},
		MaxBody:     1 << 10,
	}))

	vpc, err := s.VPC()
	require.NoError(t, err)

	_, err = vpc.Get(t.Context(), "empty")
	require.ErrorContains(t, err, "required parameter")

	_, err = vpc.Get(t.Context(), "big")
	require.ErrorContains(t, err, "exceeds")
}

// Токен из кэша отозван раньше срока при живом ключе: API отвечает 401,
// Connect перевыпускает токен у IAM, повторяет GET и кладёт свежий токен в
// кэш — следующий запуск берёт его без IAM.
func TestConnectRefreshesRevokedToken(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	var issued atomic.Int32

	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tok := "tok"
		if issued.Add(1) == 1 {
			tok = "revoked"
		}

		_, _ = w.Write([]byte(`{"access_token":"` + tok + `","expires_in":3600}`))
	}))
	t.Cleanup(iam.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	t.Cleanup(api.Close)

	for i := range 2 {
		s := &State{Printer: &output.Printer{Out: io.Discard, Err: io.Discard}}
		require.NoError(t, s.Connect(Conn{
			Config:      config.Config{KeyID: "k", KeySecret: "s", IAMAuthURL: iam.URL, VPCURL: api.URL},
			RetryDelays: []time.Duration{},
		}))

		vpc, err := s.VPC()
		require.NoError(t, err)

		_, err = vpc.Get(t.Context(), "x")
		require.NoError(t, err, "запуск %d", i+1)
	}

	assert.Equal(t, int32(2), issued.Load(), "отозванный и свежий; второй запуск — из кэша")
}

// IAM не принял ключ: запрос к API продукта не уходит, ошибка указывает на ключ.
func TestConnectNoAPIRequestWithoutToken(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	t.Cleanup(iam.Close)

	var calls atomic.Int32

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)

		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	t.Cleanup(api.Close)

	s := &State{Printer: &output.Printer{Out: io.Discard, Err: io.Discard}}
	require.NoError(t, s.Connect(Conn{
		Config:      config.Config{KeyID: "k", KeySecret: "s", IAMAuthURL: iam.URL, VPCURL: api.URL},
		RetryDelays: []time.Duration{},
	}))

	vpc, err := s.VPC()
	require.NoError(t, err)

	_, err = vpc.Get(t.Context(), "x")
	require.ErrorContains(t, err, "CLOUDRU_KEY_ID")
	assert.Equal(t, 4, output.ExitCode(err))
	assert.Zero(t, calls.Load(), "без токена — ни одного запроса к API")
}
