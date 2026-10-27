package cloud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Обход страниц списков: DBaaS, VPC и IAM — по page_token, compute — по
// offset/limit. Метод списка отдаёт все страницы одним ответом той же схемы:
// одна страница молча теряла бы хвост, и агент принял бы
// отсутствие ресурса за факт.

// Пределы одного списка (var — для тестов): 1000 страниц по 100 элементов —
// на порядки больше, чем бывает в проекте; дальше — неисправный API, и
// память CLI не должна расти без конца. Байты — потому что лимит тела
// транспорта ограничивает один ответ, но не тысячу.
var (
	maxPages     = 1000
	maxListBytes = 256 << 20
)

// ErrList — ответ списка, который нельзя склеить: null, нет поля, токен по кругу, предел.
var ErrList = i18n.Sentinel("cloud.err.list")

// pages обходит страницы списка DBaaS и отдаёт {key: элементы всех страниц}.
func pages(key string, fetch func(token string) ([]byte, error)) ([]byte, error) {
	_, _, all, err := walkTokens(key, []string{key}, fetch)
	if err != nil {
		return nil, err
	}

	return marshalList(map[string]any{key: all})
}

// tokenPages обходит страницы списка VPC или IAM и отдаёт первую страницу, где
// под ключом списка — элементы всех страниц, без токена следующей.
func tokenPages(path string, keys []string, fetch func(token string) ([]byte, error)) ([]byte, error) {
	first, key, all, err := walkTokens(path, keys, fetch)
	if err != nil {
		return nil, err
	}

	delete(first, "next_page_token")
	delete(first, "nextPageToken")
	first[key] = all

	return marshalList(first)
}

// walkTokens — обход по page_token. fetch получает токен ("" — первая
// страница). keys — имена поля списка: IAM отвечает snake_case, VPC —
// camelCase. Числа — как есть (json.Number): большие id не превращаются
// в 1.2e+18.
func walkTokens(path string, keys []string, fetch func(token string) ([]byte, error)) (first map[string]any, key string, all []any, err error) {
	all, key = []any{}, keys[0] // пустой список в --json — [], не null: jq '.x[]' на null падает
	seen := map[string]bool{}

	var b budget

	for token, n := "", 0; ; n++ {
		if n >= maxPages {
			return nil, "", nil, tooManyPages()
		}

		page, err := b.fetch(path, func() ([]byte, error) { return fetch(token) })
		if err != nil {
			return nil, "", nil, err
		}

		if first == nil {
			first = page
		}

		next, _ := page["next_page_token"].(string)
		if next == "" {
			next, _ = page["nextPageToken"].(string)
		}

		items, k, err := pageItems(page, path, next == "", keys...)
		if err != nil {
			return nil, "", nil, err
		}

		if items != nil {
			key = k
		}

		all = append(all, items...)

		if next == "" {
			return first, key, all, nil
		}

		if seen[next] {
			return nil, "", nil, i18n.Errorf("cloud.err.list_loop", ErrList, next)
		}

		seen[next], token = true, next
	}
}

// offsetPageLimit — максимум limit у списков compute (спецификация: maximum 100).
const offsetPageLimit = 100

// offsetAttempts — сколько раз обходить список заново, если он менялся во
// время обхода.
const offsetAttempts = 3

// offsetPages проходит все страницы списка compute (offset/limit, total)
// и возвращает {"items": все, "total": N}. Offset сдвигается, если между
// запросами элемент добавили или удалили: элементы повторяются или
// пропадают. Это видно по смене total между страницами или расхождению
// собранного с total — тогда обходим заново, а после offsetAttempts попыток
// отказываем. Удаление уже собранного элемента число не меняет — его ловит
// только смена total.
func offsetPages(path string, fetch func(offset, limit int) ([]byte, error)) ([]byte, error) {
	for attempt := 1; ; attempt++ {
		all, total, changed, err := walkOffsets(path, fetch)
		if err != nil {
			return nil, err
		}

		if !changed && (total < 0 || len(all) == total) {
			return marshalList(map[string]any{"items": all, "total": len(all)})
		}

		if attempt == offsetAttempts {
			return nil, i18n.Errorf("cloud.err.list_changed",
				ErrList, path, offsetAttempts, len(all), total)
		}
	}
}

// walkOffsets — один обход. total — последнее значение из ответа, -1 —
// API его не прислал; changed — total менялся между страницами.
func walkOffsets(path string, fetch func(offset, limit int) ([]byte, error)) (all []any, total int, changed bool, err error) {
	all, total = []any{}, -1

	var b budget

	for offset, n := 0, 0; ; n++ {
		if n >= maxPages {
			return nil, 0, false, tooManyPages()
		}

		page, err := b.fetch(path, func() ([]byte, error) { return fetch(offset, offsetPageLimit) })
		if err != nil {
			return nil, 0, false, err
		}

		if t, terr := strconv.Atoi(fmt.Sprint(page["total"])); terr == nil {
			changed = changed || (total >= 0 && t != total)
			total = t
		}
		// Без items страница — последняя, только если total не обещает ещё.
		items, _, err := pageItems(page, path, total < 0 || offset >= total, "items")
		if err != nil {
			return nil, 0, false, err
		}

		offset += len(items)
		all = append(all, items...)
		// Без total идём, пока страница полная.
		if len(items) == 0 || (total >= 0 && offset >= total) || (total < 0 && len(items) < offsetPageLimit) {
			return all, total, changed, nil
		}
	}
}

// budget считает байты всех страниц одного списка.
type budget struct{ used int }

// fetch запрашивает страницу и разбирает её в объект.
func (b *budget) fetch(path string, get func() ([]byte, error)) (map[string]any, error) {
	raw, err := get()
	if err != nil {
		return nil, err
	}

	if b.used += len(raw); b.used > maxListBytes {
		return nil, i18n.Errorf("cloud.err.list_size", ErrList, path, maxListBytes>>20)
	}

	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()

	var page map[string]any
	if err := d.Decode(&page); err != nil {
		return nil, i18n.Errorf("cloud.err.list_parse", ErrList, path, err)
	}

	if page == nil {
		return nil, i18n.Errorf("cloud.err.list_null", ErrList, path)
	}

	return page, nil
}

// pageItems — элементы страницы из поля с одним из имён keys и имя поля.
// Пустой список protobuf-JSON опускает, поэтому поля может не быть, но только
// на последней странице и только без другого списка в ответе: иначе API
// сменил имя поля, и «пусто» было бы ложью.
func pageItems(page map[string]any, path string, last bool, keys ...string) ([]any, string, error) {
	for _, k := range keys {
		if v, ok := page[k]; ok && v != nil {
			items, ok := v.([]any)
			if !ok {
				return nil, "", i18n.Errorf("cloud.err.list_field_type", ErrList, path, k)
			}

			return items, k, nil
		}
	}

	want := strings.Join(keys, "/")
	for k, v := range page {
		if _, ok := v.([]any); ok {
			return nil, "", i18n.Errorf("cloud.err.list_field_changed", ErrList, path, want, k)
		}
	}

	if !last {
		return nil, "", i18n.Errorf("cloud.err.list_field_missing", ErrList, path, want)
	}

	return nil, keys[0], nil
}

func tooManyPages() error {
	return i18n.Errorf("cloud.err.list_pages", ErrList, maxPages)
}

func marshalList(v map[string]any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, i18n.Errorf("cloud.err.list_marshal", ErrList, err)
	}

	return b, nil
}
