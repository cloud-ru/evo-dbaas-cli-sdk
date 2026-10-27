package clickhouse

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexInt64 — int64 из поля config кластера ClickHouse. Спецификация обещает
// число, API отдаёт строку ("100", как int64 в proto3 JSON); принимаем оба,
// отправляем числом.
type FlexInt64 int64

func (v *FlexInt64) UnmarshalJSON(b []byte) error {
	var n int64
	if err := json.Unmarshal(b, &n); err == nil {
		*v = FlexInt64(n)
		return nil
	}

	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("FlexInt64: %w", err)
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("FlexInt64: %w", err)
	}

	*v = FlexInt64(n)

	return nil
}
