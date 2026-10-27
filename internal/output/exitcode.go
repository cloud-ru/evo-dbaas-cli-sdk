package output

import (
	"context"
	"errors"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/auth"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// ExitCode — код выхода по виду сбоя, как у gh: 0 успех,
// 1 ошибка, 2 отмена (Ctrl-C), 4 ключ не задан или не принят, доступ запрещён,
// 8 --wait не дождался — операция, возможно, идёт в облаке. Ошибка со своим
// кодом реализует ExitCode() int.
func ExitCode(err error) int {
	var coded interface{ ExitCode() int }

	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled):
		return 2
	case errors.Is(err, operation.ErrPending):
		return 8
	case errors.Is(err, auth.ErrNoKey), errors.Is(err, auth.ErrKeyRejected), errors.Is(err, auth.ErrKeyForbidden):
		return 4
	case errors.As(err, &coded):
		return coded.ExitCode()
	}

	return 1
}
