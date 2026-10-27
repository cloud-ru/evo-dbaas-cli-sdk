package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// Общее у API DBaaS (ClickHouse, Redis, Kafka): списки по page_token и схема
// операции cloudru.dbaas.operation.Operation.

// ExitCode — 4 на отказ в доступе. Сбой проверки доступа
// (flakyAccess) — не отказ, код 1.
func (e *APIError) ExitCode() int {
	if (e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden) && !e.flakyAccess() {
		return 4
	}

	return 1
}

// flakyAccess — 13.10 вживую: API ClickHouse отвечал этим 403 на ~60 %
// любых запросов подряд — сбой проверки доступа, не отказ.
const flakyAccess = "failed to verify project access"

func (e *APIError) flakyAccess() bool {
	return e.Status == http.StatusForbidden && strings.Contains(e.Message, flakyAccess)
}

// dbaasPageSize отправляем в каждом списке DBaaS. В спецификации page_size
// необязателен, но Redis без него отвечает 500 (проверено 8.10.2026).
const dbaasPageSize = "100"

// dbaasOperation — схема операции, общая у трёх продуктов.
type dbaasOperation struct {
	ID          string  `json:"id"`
	ResourceID  string  `json:"resource_id"`
	Description string  `json:"description"`
	Done        bool    `json:"done"`
	Error       *rpcErr `json:"error"`
}

type rpcErr struct {
	Message string `json:"message"`
}

// ParseOperation разбирает ответ DBaaS в операцию; ok — в теле операция с id.
func ParseOperation(raw []byte) (op operation.Operation, ok bool) {
	op.Raw = raw

	var o dbaasOperation
	if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &o) != nil || o.ID == "" {
		return op, false
	}

	op.ID, op.ResourceID, op.Description, op.Done = o.ID, o.ResourceID, o.Description, o.Done
	if o.Error != nil {
		op.Failed, op.Message = true, o.Error.Message
	}

	return op, true
}

// PollError — ошибка опроса (операции, ресурса): сбой сети, 429, 5xx и сбой
// проверки доступа — временные, опрос — GET, повтор безопасен. Остальные
// 4xx — ошибка запроса, повтор не поможет.
func PollError(err error) error {
	var ae *APIError
	if err == nil || errors.As(err, &ae) && ae.Status != http.StatusTooManyRequests && ae.Status < 500 && !ae.flakyAccess() {
		return err
	}

	return operation.Transient(err)
}

// operationGetter — опрос операции DBaaS для operation.Waiter.
func operationGetter(get func(ctx context.Context, id string) ([]byte, error)) operation.Getter {
	return func(ctx context.Context, id string) (operation.Operation, error) {
		raw, err := get(ctx, id)
		if err != nil {
			return operation.Operation{}, PollError(err)
		}

		op, ok := ParseOperation(raw)
		if !ok {
			return op, errors.New(i18n.T("cloud.err.empty_operation", id))
		}

		return op, nil
	}
}
