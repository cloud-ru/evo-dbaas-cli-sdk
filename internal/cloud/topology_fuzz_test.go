package cloud

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func FuzzParseTopologyBody(f *testing.F) {
	for _, s := range []string{"", "x", "=", " ", "null", "[]", "{}", "{", `{"n":null,"s":null,"subnet_ids":null,"o":null}`, `{"n":2147483648,"s":1,"subnet_ids":[null],"o":[]}`, `{"n":-2147483649}`, `{"n":1.5}`, `{"n":1,"n":2,"s":"юникод","subnet_ids":["s1","s2"],"o":{}}`, `{"s":"` + strings.Repeat("я", 4096) + `"}`} {
		f.Add([]byte(s), int32(17), "fallback")
	}

	f.Fuzz(func(t *testing.T, raw []byte, fallback int32, text string) {
		var fields map[string]json.RawMessage

		refErr := json.Unmarshal(raw, &fields)

		body, err := parseTopologyBody(raw)
		if (err != nil) != (refErr != nil) {
			t.Fatalf("body recognition changed: %s: %v, want %v", raw, err, refErr)
		}

		if err != nil {
			if body != nil || !strings.Contains(err.Error(), "--body") {
				t.Fatalf("bad body or flag lost: %v, %v", body, err)
			}

			return
		}

		number, str := &fallback, &text
		if field, exists := fields["n"]; exists {
			number = nil
			if err := json.Unmarshal(field, &number); err != nil {
				number = nil
			}
		}

		if field, exists := fields["s"]; exists {
			str = nil
			if err := json.Unmarshal(field, &str); err != nil {
				str = nil
			}
		}

		if got := body.number("n", fallback); !reflect.DeepEqual(got, number) {
			t.Fatalf("number projection: %s -> %v, want %v", raw, got, number)
		}

		if got := body.text("s", text); !reflect.DeepEqual(got, str) {
			t.Fatalf("text projection: %s -> %v, want %v", raw, got, str)
		}

		fallbackSubnets := []string{text}

		subnets := &fallbackSubnets
		if field, exists := fields["subnet_ids"]; exists {
			// Decode elements individually: null is not a subnet string.
			subnets = nil

			var elements []json.RawMessage
			if json.Unmarshal(field, &elements) == nil && elements != nil {
				values := make([]string, 0, len(elements))

				valid := true
				for _, element := range elements {
					var value *string
					if json.Unmarshal(element, &value) != nil || value == nil {
						valid = false
						break
					}

					values = append(values, *value)
				}

				if valid {
					subnets = &values
				}
			}
		}

		if got := body.subnets(fallbackSubnets); !reflect.DeepEqual(got, subnets) {
			t.Fatalf("subnets projection: %s -> %v, want %v", raw, got, subnets)
		}

		var object topologyBody

		field, exists := fields["o"]
		if exists {
			if err := json.Unmarshal(field, &object); err != nil {
				object = nil
			}
		}

		gotObject, gotExists := body.object("o")
		if gotExists != exists || !reflect.DeepEqual(gotObject, object) {
			t.Fatalf("object projection changed: %s -> %v, %t", raw, gotObject, gotExists)
		}
		// Exercise the absent-field branch even for inputs that only contain junk.
		if got := (topologyBody{}).number("n", fallback); got == nil || *got != fallback {
			t.Fatalf("fallback lost: %v", got)
		}
	})
}

func TestTopologyInt32Boundaries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		value int64
		valid bool
	}{{-2147483649, false}, {-2147483648, true}, {-2147483647, true}, {2147483646, true}, {2147483647, true}, {2147483648, false}} {
		t.Run(fmt.Sprint(tc.value), func(t *testing.T) {
			t.Parallel()

			body, err := parseTopologyBody(fmt.Appendf(nil, `{"n":%d}`, tc.value))
			if err != nil {
				t.Fatal(err)
			}

			got := body.number("n", 17)
			if tc.valid {
				if got == nil || int64(*got) != tc.value {
					t.Fatalf("boundary changed: %v, want %d", got, tc.value)
				}
			} else if got != nil {
				t.Fatalf("overflow accepted: %d", *got)
			}
		})
	}
}
