package cloud

import (
	"encoding/json"
	"math/big"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func FuzzParseClickHouseConfig(f *testing.F) {
	typ := reflect.TypeFor[ClickHouseConfig]()
	for i := range typ.NumField() {
		key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		for _, value := range []string{"", "0", "-1", "+1", " 1", "1.0", "0x10", "010", "1_0", "00000000008", "9223372036854775807", "-9223372036854775808", "9223372036854775808", "-9223372036854775809"} {
			f.Add(key, value)
		}
	}

	for _, key := range []string{"", "=", "unknown", "ключ", strings.Repeat("x", 8192)} {
		f.Add(key, "1")
	}

	integer := regexp.MustCompile(`^[+-]?[0-9]+$`)
	f.Fuzz(func(t *testing.T, key, value string) {
		canonical := ""
		for i := range typ.NumField() {
			tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if strings.EqualFold(tag, key) {
				canonical = tag
			}
		}

		n, numeric := new(big.Int).SetString(value, 10)
		valid := canonical != "" && integer.MatchString(value) && numeric && n.IsInt64()

		got, err := ParseClickHouseConfig("fuzz-config", map[string]string{key: value})
		if !valid {
			if err == nil || got != nil || !strings.Contains(err.Error(), "--fuzz-config") {
				t.Fatalf("invalid config accepted or flag lost: %q=%q -> %v, %v", key, value, got, err)
			}

			return
		}

		if err != nil || got == nil {
			t.Fatalf("valid config rejected: %q=%q: %v", key, value, err)
		}

		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}

		var fields map[string]int64
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}

		if len(fields) != 1 || fields[canonical] != n.Int64() {
			t.Fatalf("config field lost or rounded: %s; want %s=%s", raw, canonical, n)
		}
	})
}

func TestClickHouseConfigInt64Boundaries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		value string
		valid bool
	}{{"-9223372036854775809", false}, {"-9223372036854775808", true}, {"-9223372036854775807", true}, {"9223372036854775806", true}, {"9223372036854775807", true}, {"9223372036854775808", false}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()

			got, err := ParseClickHouseConfig("config", map[string]string{"max_connections": tc.value})
			if !tc.valid {
				if err == nil || got != nil {
					t.Fatalf("overflow accepted: %v, %v", got, err)
				}

				return
			}

			if err != nil || got == nil || got.MaxConnections == nil {
				t.Fatalf("boundary rejected: %v, %v", got, err)
			}

			if value := new(big.Int).SetInt64(int64(*got.MaxConnections)).String(); value != tc.value {
				t.Fatalf("boundary rounded: %s, want %s", value, tc.value)
			}
		})
	}
}
