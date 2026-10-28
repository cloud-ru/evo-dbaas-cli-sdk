package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

const (
	defaultTTL      = time.Hour // если IAM не назвал срок
	maxResponseSize = 1 << 20
	maxIAMMessage   = 200 // рун текста ошибки IAM в stderr
	requestTimeout  = 30 * time.Second
)

var errNoToken = i18n.Sentinel("auth.err.no_token")

// IAMSource получает свежий токен у IAM — без кэша.
type IAMSource struct {
	KeyID  string
	Secret string
	URL    string // адрес обмена ключа на токен, по умолчанию — из config
	HTTP   *http.Client
	Now    func() time.Time
}

// Endpoint — адрес IAM, к которому идёт запрос.
func (s *IAMSource) Endpoint() string { return s.URL }

// Fetch обменивает ключ на токен.
func (s *IAMSource) Fetch(ctx context.Context) (Token, error) {
	if s.KeyID == "" || s.Secret == "" {
		return Token{}, ErrNoKey
	}

	body, err := json.Marshal(map[string]string{"keyId": s.KeyID, "secret": s.Secret})
	if err != nil {
		return Token{}, i18n.Errorf("auth.err.iam", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return Token{}, i18n.Errorf("auth.err.iam", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Копия клиента без перехода по редиректам: на 307/308 POST с ключом ушёл бы по Location, в том числе на http.
	hc := &http.Client{Timeout: requestTimeout}
	if s.HTTP != nil {
		c := *s.HTTP
		hc = &c
	}

	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := hc.Do(req)
	if err != nil {
		return Token{}, i18n.Errorf("auth.err.iam_request", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if resp.StatusCode != http.StatusOK {
		return Token{}, &IAMError{Status: resp.StatusCode, Body: iamMessage(resp.StatusCode, raw)}
	}

	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}

	if err := json.Unmarshal(raw, &r); err != nil || r.AccessToken == "" {
		return Token{}, errNoToken
	}

	ttl := time.Duration(r.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = defaultTTL
	}

	return Token{AccessToken: r.AccessToken, ExpiresAt: s.now().Add(ttl)}, nil
}

func (s *IAMSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// iamMessage оставляет от ответа IAM только текст ошибки (message, error_description или error из JSON): сырое тело
// в stderr не печатаем — изменись API, туда попал бы keyId. Нет текстового поля или не JSON — название кода HTTP.
func iamMessage(status int, raw []byte) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		for _, k := range []string{"message", "error_description", "error"} {
			if s, ok := m[k].(string); ok {
				if s = cleanMessage(s); s != "" {
					return s
				}
			}
		}
	}

	return http.StatusText(status)
}

// cleanMessage убирает управляющие символы — ESC-последовательности и перевод строки подделали бы вывод
// терминала — и режет текст до maxIAMMessage рун.
func cleanMessage(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, s))

	if r := []rune(s); len(r) > maxIAMMessage {
		s = string(r[:maxIAMMessage]) + "…"
	}

	return s
}
