// Package cloud — вызовы API cloud.ru поверх клиентов internal/gen. Запрос
// собирает сгенерированный клиент; ответ 2xx отдаётся телом как есть — вывод
// --json без потерь полей, которых нет в спецификации; не 2xx — *APIError.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Options — общее для клиентов всех API.
type Options struct {
	HTTPClient *http.Client
	// Auth ставит токен в запрос.
	Auth func(ctx context.Context, req *http.Request) error
}

// pageSize — размер страницы списков с pageToken (VPC, IAM).
const pageSize = 1000

// Ошибки ввода, отказанные до запроса; текст для человека — в обёртке.
var (
	ErrNotUUID    = i18n.Sentinel("cloud.err.not_uuid")
	ErrBadID      = i18n.Sentinel("cloud.err.bad_id")
	ErrBadExpires = i18n.Sentinel("cloud.err.bad_expires")
)

// APIError — ответ API не 2xx.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return i18n.T("cloud.api_error", e.Status, e.Message) }

// IsNotFound — ответ API 404.
func IsNotFound(err error) bool {
	var e *APIError

	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// read возвращает тело ответа 2xx; пустое тело — «{}». Не 2xx — *APIError,
// сообщение из поля message тела, если оно есть.
func read(resp *http.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	raw, err := body(resp)
	if err == nil && len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}

	return raw, err
}

// readBody — как read, но пустое тело 2xx остаётся пустым: разбор его падает,
// как у сгенерированного клиента.
func readBody(resp *http.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	return body(resp)
}

func body(resp *http.Response) ([]byte, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w", err) // текст как у io.ReadAll: его видит пользователь
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := string(bytes.TrimSpace(raw))

		var st struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &st) == nil && st.Message != "" {
			msg = st.Message
		}

		return nil, &APIError{Status: resp.StatusCode, Message: msg}
	}

	return raw, nil
}

func ptr[T any](v T) *T { return &v }

// optional — nil для пустой строки: необязательное поле не уходит в запрос.
func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// pathID отказывает в id, который сгенерированный клиент не передаст как есть:
// путь собирается через url.Parse, и «.», «..» схлопываются — DELETE ушёл бы
// на родительский ресурс; пустой id — на список.
func pathID(what, id string) error {
	if id == "" || id == "." || id == ".." {
		return i18n.Errorf("cloud.err.id", what, id, ErrBadID)
	}

	return nil
}

// plusAsSpace кодирует пробел в query как %20, как до перехода на
// сгенерированные клиенты: IAM не раскодирует «+» в фильтре «… AND …».
// Настоящий «+» в значении закодирован как %2B.
func plusAsSpace(_ context.Context, req *http.Request) error {
	req.URL.RawQuery = strings.ReplaceAll(req.URL.RawQuery, "+", "%20")

	return nil
}
