package cloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenPagesMergesAliasesAndPreservesFirstPage(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"service_accounts", "serviceAccounts"} {
		for _, tokenKey := range []string{"next_page_token", "nextPageToken"} {
			t.Run(key+"/"+tokenKey, func(t *testing.T) {
				t.Parallel()

				var tokens []string

				raw, err := tokenPages("accounts", []string{"service_accounts", "serviceAccounts"}, func(token string) ([]byte, error) {
					tokens = append(tokens, token)
					switch token {
					case "":
						return fmt.Appendf(nil, `{"%s":[{"id":12345678901234567890}],"%s":"next","metadata":{"keep":true},"count":17}`, key, tokenKey), nil
					case "next":
						return fmt.Appendf(nil, `{"%s":[{"id":2}],"%s":"last","count":99}`, key, tokenKey), nil
					default:
						assert.Equal(t, "last", token)
						return []byte(`{"count":100}`), nil
					}
				})
				require.NoError(t, err)
				assert.Equal(t, []string{"", "next", "last"}, tokens)
				assert.JSONEq(t, fmt.Sprintf(`{"%s":[{"id":12345678901234567890},{"id":2}],"metadata":{"keep":true},"count":17}`, key), string(raw))
			})
		}
	}
}

func TestTokenPagesEmptyListsAndMalformedPages(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body, want, bad string }{
		{"empty", `{"items":[]}`, `{"items":[]}`, ""},
		{"omitted", `{}`, `{"items":[]}`, ""},
		{"null field on last page", `{"items":null}`, `{"items":[]}`, ""},
		{"missing before end", `{"next_page_token":"next"}`, "", "non-final page"},
		{"other list", `{"renamed":[]}`, "", "response format has changed"},
		{"scalar", `{"items":42}`, "", "not a list"},
		{"object field", `{"items":{}}`, "", "not a list"},
		{"null object", `null`, "", "null instead of an object"},
		{"invalid JSON", `not JSON`, "", "cannot parse response"},
		{"array object", `[]`, "", "cannot parse response"},
		{"repeated token", `{"items":[],"nextPageToken":"same"}`, "", "repeated page token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			calls := 0

			raw, err := tokenPages("test", []string{"items"}, func(string) ([]byte, error) {
				calls++
				return []byte(tc.body), nil
			})
			if tc.bad == "" {
				require.NoError(t, err)
				assert.JSONEq(t, tc.want, string(raw))
			} else {
				require.ErrorIs(t, err, ErrList)
				require.ErrorContains(t, err, tc.bad)
				assert.Nil(t, raw)
			}

			if tc.name == "repeated token" {
				assert.Equal(t, 2, calls)
			} else {
				assert.Equal(t, 1, calls)
			}
		})
	}
}

func TestPageItemsSelectsFirstAvailableAlias(t *testing.T) {
	t.Parallel()

	list := []any{"a"}
	got, key, err := pageItems(map[string]any{"service_accounts": nil, "serviceAccounts": list}, "accounts", false, "service_accounts", "serviceAccounts")
	require.NoError(t, err)
	assert.Equal(t, list, got)
	assert.Equal(t, "serviceAccounts", key)
	got, key, err = pageItems(map[string]any{}, "accounts", true, "service_accounts", "serviceAccounts")
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, "service_accounts", key)
}

func TestPaginationBudgets(t *testing.T) {
	oldPages, oldBytes := maxPages, maxListBytes
	t.Cleanup(func() { maxPages, maxListBytes = oldPages, oldBytes })

	for _, offset := range []bool{false, true} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			for _, limit := range []int{0, 2} {
				maxPages = limit
				calls := 0
				fetch := func() ([]byte, error) {
					calls++
					return fmt.Appendf(nil, `{"items":[1],"next_page_token":"%d","total":1000}`, calls), nil
				}

				var raw []byte

				var err error
				if offset {
					raw, err = offsetPages("test", func(int, int) ([]byte, error) { return fetch() })
				} else {
					raw, err = tokenPages("test", []string{"items"}, func(string) ([]byte, error) { return fetch() })
				}

				require.ErrorIs(t, err, ErrList)
				require.ErrorContains(t, err, "pages")
				assert.Nil(t, raw)
				assert.Equal(t, limit, calls)
			}
		})
	}

	maxPages = oldPages

	const first = `{"items":[1],"next_page_token":"next"}`

	const last = `{"items":[2]}`
	for _, limit := range []int{len(first) - 1, len(first) + len(last) - 1, len(first) + len(last)} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			maxListBytes = limit
			calls := 0

			raw, err := tokenPages("test", []string{"items"}, func(token string) ([]byte, error) {
				calls++

				if token == "" {
					return []byte(first), nil
				}

				return []byte(last), nil
			})
			if limit == len(first)+len(last) {
				require.NoError(t, err)
				assert.JSONEq(t, `{"items":[1,2]}`, string(raw))
			} else {
				require.ErrorIs(t, err, ErrList)
				require.ErrorContains(t, err, "traversal stopped")
				assert.Nil(t, raw)
			}

			if limit < len(first) {
				assert.Equal(t, 1, calls)
			} else {
				assert.Equal(t, 2, calls)
			}
		})
	}
}

func TestBudgetFetchPreservesNumbersAndRequestErrors(t *testing.T) {
	t.Parallel()

	var b budget

	page, err := b.fetch("test", func() ([]byte, error) { return []byte(`{"id":12345678901234567890}`), nil })
	require.NoError(t, err)
	assert.Equal(t, json.Number("12345678901234567890"), page["id"])

	used := b.used
	requestErr := errors.New("connection lost")
	page, err = b.fetch("test", func() ([]byte, error) { return nil, requestErr })
	require.ErrorIs(t, err, requestErr)
	require.NotErrorIs(t, err, ErrList) // Ошибки запроса пока передаются без ErrList.
	assert.Nil(t, page)
	assert.Equal(t, used, b.used)

	for _, offset := range []bool{false, true} {
		if offset {
			_, err = offsetPages("test", func(int, int) ([]byte, error) { return nil, requestErr })
		} else {
			_, err = tokenPages("test", []string{"items"}, func(string) ([]byte, error) { return nil, requestErr })
		}

		require.ErrorIs(t, err, requestErr)
		require.NotErrorIs(t, err, ErrList)
	}
}

func TestOffsetPagesUsesLimitAndOffsets(t *testing.T) {
	t.Parallel()

	for _, total := range []bool{false, true} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			t.Parallel()

			var offsets []int

			raw, err := offsetPages("test", func(offset, limit int) ([]byte, error) {
				assert.Equal(t, 100, limit)

				offsets = append(offsets, offset)

				count := 100
				if offset == 100 {
					count = 2
				}

				items := make([]int, count)
				for i := range items {
					items[i] = offset + i
				}

				page := map[string]any{"items": items}
				if total {
					page["total"] = 102
				}

				data, err := json.Marshal(page)
				require.NoError(t, err)

				return data, nil
			})
			require.NoError(t, err)
			assert.Equal(t, []int{0, 100}, offsets)

			var result struct {
				Items []int `json:"items"`
				Total int   `json:"total"`
			}
			require.NoError(t, json.Unmarshal(raw, &result))
			assert.Len(t, result.Items, 102)
			assert.Equal(t, 101, result.Items[101])
			assert.Equal(t, 102, result.Total)
		})
	}

	for _, body := range []string{`{}`, `{"total":0}`, `{"items":[],"total":0}`} {
		raw, err := offsetPages("test", func(offset, limit int) ([]byte, error) {
			assert.Zero(t, offset)
			assert.Equal(t, 100, limit)

			return []byte(body), nil
		})
		require.NoError(t, err)
		assert.JSONEq(t, `{"items":[],"total":0}`, string(raw))
	}
}

func TestOffsetPagesRetriesChangingOrMismatchedTotal(t *testing.T) {
	t.Parallel()

	for _, mismatch := range []bool{false, true} {
		for _, recoverList := range []bool{false, true} {
			t.Run(fmt.Sprintf("mismatch=%t/recover=%t", mismatch, recoverList), func(t *testing.T) {
				t.Parallel()

				attempts, calls := 0, 0

				raw, err := offsetPages("subnets", func(offset, limit int) ([]byte, error) {
					calls++

					assert.Equal(t, 100, limit)

					if offset == 0 {
						attempts++
					}

					if recoverList && attempts == 2 {
						assert.Zero(t, offset)
						return []byte(`{"items":[1],"total":1}`), nil
					}

					if mismatch {
						assert.Zero(t, offset)
						return []byte(`{"items":[1,2],"total":1}`), nil
					}

					if offset == 0 {
						return []byte(`{"items":[1],"total":2}`), nil
					}

					assert.Equal(t, 1, offset)

					return []byte(`{"items":[2],"total":1}`), nil
				})
				if recoverList {
					require.NoError(t, err)
					assert.JSONEq(t, `{"items":[1],"total":1}`, string(raw))
					assert.Equal(t, 2, attempts)
				} else {
					require.ErrorIs(t, err, ErrList)
					require.ErrorContains(t, err, "changed during traversal (attempts: 3)")
					assert.Nil(t, raw)
					assert.Equal(t, 3, attempts)
				}

				wantCalls := attempts
				if !mismatch {
					wantCalls *= 2
					if recoverList {
						wantCalls--
					}
				}

				assert.Equal(t, wantCalls, calls)
			})
		}
	}
}

// Удаление или вставка во время обхода сдвигают offset, а число собранного
// всё равно сходится с итоговым total: элемент пропал бы или повторился.
// Видно это только по смене total между страницами — обход идёт заново.
func TestOffsetPagesRetriesWhenTotalChangesButCountMatches(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		n      int
		change func(ids []int) []int
	}{
		{"удаление", 230, func(ids []int) []int { return append(ids[:5:5], ids[6:]...) }},
		{"вставка", 150, func(ids []int) []int { return append([]int{-1}, ids...) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ids := make([]int, tc.n)
			for i := range ids {
				ids[i] = i
			}

			changed, attempts := false, 0

			raw, err := offsetPages("subnets", func(offset, limit int) ([]byte, error) {
				if offset == 0 {
					attempts++
				}

				if offset == 100 && !changed {
					ids, changed = tc.change(ids), true
				}

				return json.Marshal(map[string]any{"items": ids[offset:min(offset+limit, len(ids))], "total": len(ids)})
			})
			require.NoError(t, err)
			assert.Equal(t, 2, attempts)

			var result struct {
				Items []int `json:"items"`
				Total int   `json:"total"`
			}
			require.NoError(t, json.Unmarshal(raw, &result))
			assert.Equal(t, ids, result.Items)
			assert.Equal(t, len(ids), result.Total)
		})
	}
}

func TestOffsetPagesRejectsInvalidPages(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{"total":1}`, `{"other":[]}`, `{"items":false}`, `null`, `not JSON`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			raw, err := offsetPages("subnets", func(int, int) ([]byte, error) { return []byte(body), nil })
			require.ErrorIs(t, err, ErrList)
			assert.Nil(t, raw)
		})
	}
}

// Страница без items до total — сразу «не последняя», без повторных обходов.
func TestOffsetPageWithoutItemsBeforeTotalIsNotLast(t *testing.T) {
	t.Parallel()

	calls := 0
	_, err := offsetPages("subnets", func(int, int) ([]byte, error) {
		calls++
		return []byte(`{"total":230}`), nil
	})
	require.ErrorIs(t, err, ErrList)
	require.ErrorContains(t, err, "non-final page")
	assert.Equal(t, 1, calls)
}

func TestMarshalListWrapsEncodingError(t *testing.T) {
	t.Parallel()

	raw, err := marshalList(map[string]any{"items": make(chan int)})
	require.ErrorIs(t, err, ErrList)
	require.ErrorContains(t, err, "assembling pages")
	assert.Nil(t, raw)
}
