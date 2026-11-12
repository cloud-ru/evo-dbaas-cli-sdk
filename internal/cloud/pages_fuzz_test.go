package cloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func FuzzPageItems(f *testing.F) {
	for _, s := range []string{"", "null", "[]", "{}", "{", `{"items":[]}`, `{"items":null}`, `{"items":[9999999999999999999999999999999999]}`, `{"items":1}`, `{"items":null,"alias":["я"]}`, `{"items":[1],"alias":[2]}`, `{"renamed":[]}`, `{"items":["` + strings.Repeat("я", 4096) + `"]}`} {
		f.Add([]byte(s), true)
		f.Add([]byte(s), false)
	}

	f.Fuzz(func(t *testing.T, raw []byte, last bool) {
		var page map[string]any

		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()

		if d.Decode(&page) != nil {
			page = map[string]any{"items": string(raw)}
		}

		got, key, err := pageItems(page, "fuzz-page", last, "items", "alias")

		var expected []any

		selected := "items"

		valid, found := true, false
		for _, name := range []string{"items", "alias"} {
			if value := page[name]; value != nil {
				selected, found = name, true
				expected, valid = value.([]any)

				break
			}
		}

		if !found {
			valid = last
			for _, value := range page {
				if _, list := value.([]any); list {
					valid = false
				}
			}
		}

		if !valid {
			if !errors.Is(err, ErrList) || got != nil || key != "" {
				t.Fatalf("invalid list accepted: %s -> %v, %q, %v", raw, got, key, err)
			}
		} else if err != nil || key != selected || !reflect.DeepEqual(got, expected) {
			t.Fatalf("list/alias changed: %s -> %v, %q, %v; want %v, %q", raw, got, key, err, expected, selected)
		}
	})
}

func FuzzWalkTokens(f *testing.F) {
	for _, s := range []string{"", "x", "null", "[]", "{}", "{", `{"items":[]}`, `{"items":[123456789012345678901234567890]}`, `{"items":null}`, `{"items":"x"}`, `{"next_page_token":"next"}`, `{"items":[],"next_page_token":"repeat"}`, `{"items":[1],"next_page_token":"a","nextPageToken":"b"}`, `{"items":["` + strings.Repeat("я", 4096) + `"]}`} {
		for mode := uint8(0); mode < 4; mode++ {
			f.Add([]byte(s), "токен", mode)
		}
	}

	f.Fuzz(func(t *testing.T, raw []byte, token string, mode uint8) {
		// Generated chains keep each page small so they test the page budget.
		// Mode zero still feeds the entire arbitrary response to the parser.
		token = token[:min(len(token), 4096)]
		payload := string(raw[:min(len(raw), 4096)])
		calls := 0

		var (
			responses [][]byte
			requests  []string
		)

		expected := []any{}

		first, key, all, err := walkTokens("fuzz-list", []string{"items"}, func(request string) ([]byte, error) {
			// Fail from the callback if a broken walker would otherwise loop forever.
			if calls >= maxPages {
				t.Fatalf("walker exceeded %d calls", maxPages)
			}

			requests = append(requests, request)
			calls++

			response := raw
			if mode%4 != 0 {
				next := ""
				switch mode % 4 {
				case 1: // finite chain, even if the input token is empty
					if calls < 3 {
						next = fmt.Sprintf("%s/%d", token, calls)
					}
				case 2: // repeated or empty token
					next = token
				case 3: // unbounded distinct tokens must hit the page budget
					next = fmt.Sprintf("%s/%d", token, calls)
				}

				var marshalErr error

				response, marshalErr = json.Marshal(map[string]any{"items": []any{json.Number(fmt.Sprint(calls)), payload}, "next_page_token": next})
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
			}

			responses = append(responses, response)

			return response, nil
		})

		if calls == 0 || requests[0] != "" {
			t.Fatalf("first fetch lost: %v", requests)
		}

		for i, response := range responses {
			var page map[string]any

			d := json.NewDecoder(bytes.NewReader(response))
			d.UseNumber()

			valid := json.Valid(response) && d.Decode(&page) == nil && page != nil
			if !valid {
				if err == nil {
					t.Fatalf("malformed response accepted: %q", response)
				}

				continue
			}

			next, _ := page["next_page_token"].(string)
			if next == "" {
				next, _ = page["nextPageToken"].(string)
			}

			if i+1 < len(requests) && requests[i+1] != next {
				t.Fatalf("next token changed: %q -> %q", next, requests[i+1])
			}

			if items, ok := page["items"].([]any); ok {
				expected = append(expected, items...)
			}
		}

		if err != nil {
			if !errors.Is(err, ErrList) || first != nil || key != "" || all != nil {
				t.Fatalf("partial list leaked: %v, %q, %v, %v", first, key, all, err)
			}
		} else if first == nil || key != "items" || all == nil || !reflect.DeepEqual(all, expected) {
			t.Fatalf("items lost/rounded: %v, %q, %v; want %v", first, key, all, expected)
		}

		switch mode % 4 {
		case 0:
			if calls > 2 {
				t.Fatalf("constant response repeated more than twice: %d", calls)
			}
		case 1:
			if err != nil || calls != 3 || len(all) != 6 {
				t.Fatalf("finite chain lost: calls=%d, items=%v, err=%v", calls, all, err)
			}
		case 2:
			wantCalls := 2
			if token == "" {
				wantCalls = 1
			}

			if calls != wantCalls || (err == nil) != (token == "") {
				t.Fatalf("cycle/empty token: calls=%d, err=%v", calls, err)
			}
		case 3:
			if calls != maxPages || !errors.Is(err, ErrList) {
				t.Fatalf("page budget lost: calls=%d, err=%v", calls, err)
			}
		}
	})
}

func FuzzWalkOffsets(f *testing.F) {
	for _, s := range []string{"", "x", "null", "[]", "{}", "{", `{"items":[]}`, `{"items":[999999999999999999999999],"total":1}`, `{"items":[1],"total":999999999999999999999999999999999}`, `{"items":[],"total":-1}`, `{"items":false}`, `{"total":1}`, `{"items":["` + strings.Repeat("я", 4096) + `"],"total":1}`} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		calls := 0

		var source map[string]any

		sourceDecoder := json.NewDecoder(bytes.NewReader(raw))
		sourceDecoder.UseNumber()
		// For a repeated page the next offset must advance by exactly its size.
		if sourceDecoder.Decode(&source) != nil {
			source = nil
		}

		sourceItems, _ := source["items"].([]any)
		all, _, _, err := walkOffsets("fuzz-offset", func(offset, limit int) ([]byte, error) {
			if calls >= maxPages || offset != calls*len(sourceItems) || limit != offsetPageLimit || (calls == 0 && offset != 0) {
				t.Fatalf("offset/budget changed: calls=%d, offset=%d, limit=%d", calls, offset, limit)
			}

			calls++

			return raw, nil
		})

		if calls < 1 {
			t.Fatal("page was never fetched")
		}

		if err != nil {
			if !errors.Is(err, ErrList) || all != nil {
				t.Fatalf("partial offset list leaked: %v, %v", all, err)
			}

			return
		}

		var page map[string]any

		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()

		if !json.Valid(raw) || d.Decode(&page) != nil || page == nil {
			t.Fatalf("malformed offset response accepted: %q", raw)
		}

		items, _, itemErr := pageItems(page, "fuzz-offset", true, "items")
		if itemErr != nil || all == nil || len(all) != calls*len(items) {
			t.Fatalf("offset items lost: %v, calls=%d, items=%v", all, calls, items)
		}

		for i, item := range all {
			if !reflect.DeepEqual(item, items[i%len(items)]) {
				t.Fatalf("offset item rounded/changed: %v, want %v", item, items[i%len(items)])
			}
		}
	})
}

func TestPaginationBudgetBoundaries(t *testing.T) {
	// Serial: existing tests also adjust these package-level budgets.
	oldPages, oldBytes := maxPages, maxListBytes
	for _, pages := range []int{oldPages - 1, oldPages, oldPages + 1} {
		t.Run(fmt.Sprintf("pages/%d", pages), func(t *testing.T) {
			calls := 0

			_, _, all, err := walkTokens("boundary", []string{"items"}, func(string) ([]byte, error) {
				calls++

				next := ""
				if calls < pages {
					next = fmt.Sprint(calls)
				}

				return fmt.Appendf(nil, `{"items":[1],"next_page_token":%q}`, next), nil
			})
			if pages <= oldPages {
				if err != nil || calls != pages || len(all) != pages {
					t.Fatalf("page boundary changed: calls=%d, len=%d, err=%v", calls, len(all), err)
				}
			} else if !errors.Is(err, ErrList) || calls != oldPages || all != nil {
				t.Fatalf("page overflow accepted: calls=%d, items=%v, err=%v", calls, all, err)
			}
		})
	}
	// Keep the production byte limit, padding the accounting rather than
	// allocating three 256 MiB fixtures.
	raw := []byte(`{"items":[1]}`)
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprintf("bytes/%d", delta), func(t *testing.T) {
			b := budget{used: oldBytes - len(raw) + delta}

			page, err := b.fetch("boundary", func() ([]byte, error) { return raw, nil })
			if delta <= 0 {
				if err != nil || page == nil || b.used != oldBytes+delta {
					t.Fatalf("byte boundary changed: used=%d, page=%v, err=%v", b.used, page, err)
				}
			} else if !errors.Is(err, ErrList) || page != nil {
				t.Fatalf("byte overflow accepted: %v, %v", page, err)
			}
		})
	}
}

func TestPaginationWhitespace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw string }{
		{"lf", "{\"items\":[]}\n"},
		{"crlf-tab-space", "{\"items\":[]}\r\n\t "},
		{"leading-space", " {\"items\":[]}"},
	} {
		for _, offset := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/offset=%t", tc.name, offset), func(t *testing.T) {
				t.Parallel()

				calls := 0
				fetch := func() ([]byte, error) {
					calls++
					return []byte(tc.raw), nil
				}

				var (
					all []any
					err error
				)
				if offset {
					all, _, _, err = walkOffsets("whitespace", func(start, limit int) ([]byte, error) {
						if start != 0 || limit != offsetPageLimit {
							t.Fatalf("unexpected offset/limit: %d/%d", start, limit)
						}

						return fetch()
					})
				} else {
					var (
						first map[string]any
						key   string
					)

					first, key, all, err = walkTokens("whitespace", []string{"items"}, func(token string) ([]byte, error) {
						if token != "" {
							t.Fatalf("unexpected token: %q", token)
						}

						return fetch()
					})
					if err == nil && (first == nil || key != "items") {
						t.Fatalf("page/key lost: %v, %q", first, key)
					}
				}

				if err != nil || all == nil || len(all) != 0 || calls != 1 {
					t.Fatalf("valid whitespace page rejected: calls=%d, items=%v, err=%v", calls, all, err)
				}
			})
		}
	}
}
