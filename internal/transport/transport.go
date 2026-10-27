// Package transport — обвязка http.Client для API cloud.ru: повтор чтения при
// временном сбое, перевыпуск токена на 401, предел тела ответа и тело для
// ответов-ошибок без тела. Команды о ней не знают.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// DefaultRetryDelays — паузы между повторами GET: 14.10 ~00:35 API Redis
// полчаса отвечал 403 «failed to verify project access», и прогон потерял
// 9 шагов из 21.
var DefaultRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

// DefaultMaxBody — предел тела ответа. Самый большой список (1000 элементов
// страницы IAM) — сотни килобайт; больше — сбой шлюза или не тот адрес.
const DefaultMaxBody int64 = 64 << 20

// Options — настройки New; нулевые поля берут значения по умолчанию.
type Options struct {
	// Refresh перевыпускает токен после 401; nil — без перевыпуска.
	Refresh func(ctx context.Context) (string, error)
	// RetryDelays — паузы между повторами GET, их число — число повторов; nil — DefaultRetryDelays.
	RetryDelays []time.Duration
	// MaxBody — предел тела ответа в байтах; 0 — DefaultMaxBody.
	MaxBody int64
	// EmptyBodyHint — текст для ответа-ошибки без тела и без grpc-message.
	EmptyBodyHint func(code int) string
}

// New собирает цепочку: перевыпуск на 401 → повтор GET → тело для ошибок → предел тела → base.
// Перевыпуск снаружи повтора: отказ IAM не временный сбой, повтор GET снова пошёл бы в IAM.
func New(base http.RoundTripper, o Options) http.RoundTripper {
	if o.RetryDelays == nil {
		o.RetryDelays = DefaultRetryDelays
	}

	if o.MaxBody == 0 {
		o.MaxBody = DefaultMaxBody
	}

	next := limitBody(base.RoundTrip, o.MaxBody)

	next = retryGet(emptyErrorBody(next, o.EmptyBodyHint), o.RetryDelays)
	if o.Refresh != nil {
		next = refreshOn401(next, o.Refresh)
	}

	return next
}

// roundTripFunc — звено цепочки; звенья — функции, а не http.RoundTripper:
// ошибку оборачивает то звено, где она возникла, а не каждое по пути.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// retryGet повторяет GET при сбое транспорта, 429, 5xx и 403 «failed to verify
// project access». Только GET — чтение повторять безопасно; создание при
// обрыве сети иначе дало бы два кластера.
func retryGet(next roundTripFunc, delays []time.Duration) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		resp, err := next(req)
		for _, d := range delays {
			if req.Method != http.MethodGet || !transient(resp, err) {
				break
			}

			if resp != nil {
				resp.Body.Close()
			}

			select {
			case <-req.Context().Done():
				return nil, context.Cause(req.Context())
			case <-time.After(d):
			}

			resp, err = next(req.Clone(req.Context()))
		}

		return resp, err
	}
}

// transient читает тело 403, чтобы отличить сбой проверки доступа от
// отказа, и возвращает его на место.
func transient(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}

	if resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))

		return bytes.Contains(body, []byte("failed to verify project access"))
	}

	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

// refreshOn401 перевыпускает токен, если API ответил 401: токен из кэша мог
// быть отозван раньше срока при живом ключе. GET повторяется один раз со
// свежим токеном; создание и правка — нет: 401 почти
// наверняка значит «не выполнено», но API этого не обещает. Если IAM откажет,
// агент увидит причину от IAM, а не от API продукта.
func refreshOn401(next roundTripFunc, refresh func(ctx context.Context) (string, error)) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		resp, err := next(req)
		if err != nil || resp.StatusCode != http.StatusUnauthorized || req.Header.Get("Authorization") == "" {
			return resp, err
		}

		tok, rerr := refresh(req.Context())
		if rerr != nil {
			resp.Body.Close()
			return nil, rerr
		}

		resp.Body.Close()

		if req.Method != http.MethodGet {
			return nil, errors.New(i18n.T("transport.err.token_refreshed", req.Method))
		}

		retry := req.Clone(req.Context())
		retry.Header.Set("Authorization", "Bearer "+tok)

		return next(retry)
	}
}

// emptyErrorBody подставляет тело в ответы-ошибки без тела. Redis и Kafka на
// неполный запрос отвечают 400 с Content-Type application/json и пустым телом:
// сгенерированный клиент падает на разборе («unexpected end of JSON input»),
// и агент не видит ни кода, ни причины. Причина при этом есть — в заголовке
// grpc-message.
func emptyErrorBody(next roundTripFunc, hint func(code int) string) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		resp, err := next(req)
		if err != nil || resp.StatusCode < 400 {
			return resp, err
		}

		raw, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if rerr != nil {
			return nil, i18n.Errorf("transport.err.read_error_body", rerr)
		}

		if len(bytes.TrimSpace(raw)) == 0 {
			// Причину ошибки шлюз кладёт в заголовок grpc-message, не в тело.
			msg := resp.Header.Get("Grpc-Message")
			if msg == "" && hint != nil {
				msg = hint(resp.StatusCode)
			} else if m, err := url.PathUnescape(msg); err == nil {
				msg = m
			}

			raw, _ = json.Marshal(map[string]string{"message": msg})

			resp.Header.Set("Content-Type", "application/json")
		}

		resp.Body = io.NopCloser(bytes.NewReader(raw))
		resp.ContentLength = int64(len(raw))

		return resp, nil
	}
}

// limitBody обрывает чтение тела ответа больше max ошибкой, а не усечением:
// io.ReadAll здесь и в сгенерированных клиентах иначе съел бы память процесса
// агента.
func limitBody(next roundTripFunc, maxBytes int64) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		resp, err := next(req)
		if err != nil {
			return nil, err
		}

		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(io.LimitReader(resp.Body, maxBytes), overflow{resp.Body, maxBytes}), resp.Body}

		return resp, nil
	}
}

// overflow читается после первых max байт: ещё один байт в теле — ошибка.
type overflow struct {
	r   io.Reader
	max int64
}

func (o overflow) Read([]byte) (int, error) {
	var b [1]byte

	n, err := o.r.Read(b[:])
	switch {
	case n > 0:
		return 0, errors.New(i18n.T("transport.err.body_size", o.max>>20))
	case err == nil:
		return 0, nil
	case errors.Is(err, io.EOF):
		return 0, io.EOF
	default:
		return 0, i18n.Errorf("transport.err.read_body", err)
	}
}
