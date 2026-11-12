package iam

import (
	"strings"
	"testing"
)

func FuzzShellValue(f *testing.F) {
	for _, s := range []string{"", "x", "=", "a=b=c", " ", "ключ", "'", "'\\''", "$(touch PWNED);`id`", "a\nb", "\x00", "*", "~", strings.Repeat("'я", 4096)} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		word := shellValue(input)
		// Decode a single shell word, accepting only literal safe bytes, quoted
		// spans and escaped apostrophes. No expansion or command syntax is allowed.
		var decoded strings.Builder

		quoted := false
		for i := 0; i < len(word); i++ {
			c := word[i]
			switch {
			case c == '\'':
				quoted = !quoted
			case quoted:
				decoded.WriteByte(c)
			case c == '\\' && i+1 < len(word) && word[i+1] == '\'':
				decoded.WriteByte('\'')

				i++
			case strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-+/=:@", rune(c)):
				decoded.WriteByte(c)
			default:
				t.Fatalf("active shell syntax outside quotes: %q", word)
			}
		}

		if word == "" || quoted || decoded.String() != input {
			t.Fatalf("shell word does not round trip: %q -> %q -> %q", input, word, decoded.String())
		}
	})
}
