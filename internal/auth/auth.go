// Package auth обменивает ключ доступа cloud.ru на Bearer-токен и кэширует его.
//
// Три части: IAMSource получает токен у IAM, CachedSource отдаёт его из
// хранилища, пока не истёк, FileStore хранит на диске. Ошибки — sentinel-значения;
// текст пользователю пишет верхний слой.
//
// Токен живёт 1 час (https://cloud.ru/docs/clickhouse/ug/topics/api-ref__authentication),
// поэтому кэш избавляет от лишнего запроса к IAM на каждую команду.
package auth

import (
	"time"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Token — токен доступа и момент, когда он перестаёт действовать.
type Token struct {
	AccessToken string
	ExpiresAt   time.Time
}

var (
	// ErrNoKey — ключ доступа не задан.
	ErrNoKey = i18n.Sentinel("auth.err.no_key")
	// ErrKeyRejected — IAM не принял ключ (401).
	ErrKeyRejected = i18n.Sentinel("auth.err.key_rejected")
	// ErrKeyForbidden — ключ распознан, но токен не выдан (403).
	ErrKeyForbidden = i18n.Sentinel("auth.err.key_forbidden")
	// ErrRateLimited — IAM ограничил частоту запросов (429).
	ErrRateLimited = i18n.Sentinel("auth.err.rate_limited")
)

// IAMError — IAM ответил не 200. Unwrap отдаёт sentinel по коду (или nil для прочих кодов).
type IAMError struct {
	Status int
	Body   string
}

func (e *IAMError) Error() string { return i18n.T("auth.iam_error", e.Status, e.Body) }

func (e *IAMError) Unwrap() error {
	switch e.Status {
	case 401:
		return ErrKeyRejected
	case 403:
		return ErrKeyForbidden
	case 429:
		return ErrRateLimited
	}

	return nil
}
