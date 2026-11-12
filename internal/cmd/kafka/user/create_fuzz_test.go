package user

import (
	"regexp"
	"strings"
	"testing"
)

func FuzzValidName(f *testing.F) {
	for _, s := range []string{"", "a", "0", "A", "_", "a-a", "=", "a=a", "a b", "имя", "é", "\xff", strings.Repeat("a", 8192)} {
		f.Add(s)
	}

	alphabet := regexp.MustCompile(`^[a-z0-9]+$`)
	f.Fuzz(func(t *testing.T, name string) {
		if got, want := validName(name), alphabet.MatchString(name); got != want {
			t.Fatalf("name %q: got %t, want %t", name, got, want)
		}
	})
}

func TestValidNameBoundaries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		valid bool
	}{{"", false}, {"a", true}, {"aa", true}, {"`", false}, {"b", true}, {"y", true}, {"z", true}, {"{", false}, {"/", false}, {"0", true}, {"1", true}, {"8", true}, {"9", true}, {":", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := validName(tc.name); got != tc.valid {
				t.Fatalf("name %q: got %t, want %t", tc.name, got, tc.valid)
			}
		})
	}
}
