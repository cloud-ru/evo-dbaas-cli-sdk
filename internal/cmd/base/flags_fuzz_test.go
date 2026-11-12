package base

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func FuzzParseKV(f *testing.F) {
	for _, seed := range [][2]string{{"", ""}, {"a", "=v"}, {"=", "k="}, {"k=a=b", "k=last=part"}, {" = ", "ключ=значение"}, {"x=" + strings.Repeat("я=", 4096), "z=\x00"}} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		want := map[string]string{}

		valid := true
		for _, s := range []string{a, b} {
			i := strings.IndexByte(s, '=')
			if i <= 0 {
				valid = false
			} else {
				want[s[:i]] = s[i+1:]
			}
		}

		got, err := ParseKV("fuzz-kv", []string{a, b})
		if !valid {
			var exit *ExitError
			if !errors.As(err, &exit) || exit.Code != 1 || !strings.Contains(err.Error(), "--fuzz-kv") || got != nil {
				t.Fatalf("invalid pair accepted or flag lost: %q %q -> %v, %v", a, b, got, err)
			}
		} else if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("values or last duplicate lost: %q %q -> %v, %v; want %v", a, b, got, err, want)
		}
	})
}

func FuzzOneOf(f *testing.F) {
	for _, s := range []string{"", "x", "=", "MASTER_REPLICA", " standalone ", "ЮНИКОД", strings.Repeat("x", 8192)} {
		f.Add(s, "custom-value")
	}

	f.Fuzz(func(t *testing.T, input, custom string) {
		canonical := strings.ReplaceAll(strings.ToLower(custom), "_", "-")
		allowed := []string{"standalone", "master-replica", canonical}
		normalized := strings.ReplaceAll(strings.ToLower(input), "_", "-")
		wantOK := normalized == allowed[0] || normalized == allowed[1] || normalized == canonical

		got, err := OneOf("fuzz-enum", input, allowed...)
		if wantOK {
			if err != nil || got != normalized {
				t.Fatalf("normalization changed: %q -> %q, %v; want %q", input, got, err, normalized)
			}
		} else if err == nil || got != "" || !strings.Contains(err.Error(), "--fuzz-enum") {
			t.Fatalf("unknown enum accepted or flag lost: %q -> %q, %v", input, got, err)
		}

		got, err = OneOf("fuzz-enum", custom, allowed...)
		if err != nil || got != canonical {
			t.Fatalf("canonical member rejected: %q -> %q, %v", custom, got, err)
		}
	})
}

func FuzzBody(f *testing.F) {
	for _, s := range []string{"", "x", "@", "=", "null", "[]", "{}", "{", "ключ=значение", "a\x00b", strings.Repeat("x", 8192)} {
		f.Add(s)
	}

	path := filepath.Join(f.TempDir(), "body")
	f.Fuzz(func(t *testing.T, input string) {
		// Paths stay inside the fixture: fuzzing must not read arbitrary host files.
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}

		got, err := Body("@" + path)
		if err != nil || !bytes.Equal(got, []byte(input)) {
			t.Fatalf("file bytes changed: %q, %v", got, err)
		}

		if !strings.HasPrefix(input, "@") {
			got, err = Body(input)
			if err != nil || !bytes.Equal(got, []byte(input)) || (input == "" && got != nil) {
				t.Fatalf("literal bytes changed: %q -> %q, %v", input, got, err)
			}
		} else {
			got, err = Body("@" + path + ".missing")

			var exit *ExitError
			if !errors.As(err, &exit) || exit.Code != 1 || got != nil || !strings.Contains(err.Error(), "--body") {
				t.Fatalf("missing file accepted or flag lost: %q, %v", got, err)
			}
		}
	})
}
