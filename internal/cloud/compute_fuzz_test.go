package cloud

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func FuzzParseUUID(f *testing.F) {
	for _, s := range []string{"", "x", "=", " ", "юникод", "00000000-0000-0000-0000-000000000000", "123e4567-e89b-12d3-a456-426614174000", "123E4567E89B12D3A456426614174000", "{123e4567-e89b-12d3-a456-426614174000}", "urn:uuid:123e4567-e89b-12d3-a456-426614174000", "a123e4567-e89b-12d3-a456-426614174000b", " 123e4567-e89b-12d3-a456-426614174000", strings.Repeat("f", 8192)} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Accepted: canonical, compact, braced and URN forms. All accepted
		// forms must preserve the 128 bits; invalid forms carry our sentinel.
		// 38-byte forms with anything but braces around them are rejected.
		value := input

		nonstandardWrapper := len(input) == 38 && (input[0] != '{' || input[37] != '}')
		switch len(value) {
		case 45:
			if strings.EqualFold(value[:9], "urn:uuid:") {
				value = value[9:]
			}
		case 38:
			value = value[1:37]
		}

		canonical := value
		if len(value) == 36 && value[8] == '-' && value[13] == '-' && value[18] == '-' && value[23] == '-' {
			canonical = value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
		}

		valid := len(canonical) == 32 && strings.Trim(canonical, "0123456789abcdefABCDEF") == ""

		got, err := parseUUID("fuzz-resource", input)
		if !valid || nonstandardWrapper {
			if !errors.Is(err, ErrNotUUID) || got != [16]byte{} || !strings.Contains(err.Error(), "fuzz-resource") {
				t.Fatalf("invalid UUID accepted or context lost: %q -> %v, %v", input, got, err)
			}
		} else if err != nil || strings.ReplaceAll(got.String(), "-", "") != strings.ToLower(canonical) {
			t.Fatalf("UUID bits changed: %q -> %v, %v", input, got, err)
		}
	})
}

func TestParseUUIDRejectsForeignWrappers(t *testing.T) {
	t.Parallel()

	const id = "123e4567-e89b-12d3-a456-426614174000"
	for _, input := range []string{"a" + id + "b", "(" + id + ")", "{" + id + ")", "(" + id + "}", " " + id + " ", "\"" + id + "\""} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			got, err := parseUUID("resource", input)
			if !errors.Is(err, ErrNotUUID) || got != [16]byte{} || !strings.Contains(err.Error(), "resource") {
				t.Fatalf("wrapped UUID accepted: %q -> %v, %v", input, got, err)
			}
		})
	}
}

func TestParseUUIDLengthBoundaries(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{"123e4567e89b12d3a456426614174000", "123e4567-e89b-12d3-a456-426614174000", "{123e4567-e89b-12d3-a456-426614174000}", "urn:uuid:123e4567-e89b-12d3-a456-426614174000"} {
		for _, delta := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("%d/%d", len(valid), delta), func(t *testing.T) {
				t.Parallel()

				input := valid
				if delta < 0 {
					input = input[:len(input)-1]
				} else if delta > 0 {
					input += "0"
				}

				got, err := parseUUID("resource", input)
				if delta == 0 {
					if err != nil || got.String() != "123e4567-e89b-12d3-a456-426614174000" {
						t.Fatalf("valid UUID changed: %v, %v", got, err)
					}
				} else if !errors.Is(err, ErrNotUUID) || got != [16]byte{} {
					t.Fatalf("wrong UUID length accepted: %q -> %v, %v", input, got, err)
				}
			})
		}

		for _, prefix := range []string{"0", " "} {
			t.Run(fmt.Sprintf("%d/prefix/%q", len(valid), prefix), func(t *testing.T) {
				t.Parallel()

				input := prefix + valid

				got, err := parseUUID("resource", input)
				if !errors.Is(err, ErrNotUUID) || got != [16]byte{} {
					t.Fatalf("prefixed UUID accepted: %q -> %v, %v", input, got, err)
				}
			})
		}
	}
}
