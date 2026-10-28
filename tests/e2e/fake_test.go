package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
)

// reply — ответ фейка на один маршрут «МЕТОД /продукт/путь».
type reply struct {
	status int
	body   string
	header map[string]string
}

// Ответы по умолчанию: мутация продукта DBaaS возвращает операцию, опрос операции — завершённую.
const (
	opPending = `{"id":"op1","done":false,"description":"Создание кластера","resource_id":"c1"}`
	opDone    = `{"id":"op1","done":true,"description":"Создание кластера","resource_id":"c1"}`
)

var dbaasProducts = map[string]bool{"clickhouse": true, "redis": true, "kafka": true}

// fakeCloud — все API cloud.ru на одном сервере, продукт — первый сегмент пути
// (/clickhouse, /redis, /kafka, /compute, /vpc, /iamapi; токен — /iam/token).
type fakeCloud struct {
	srv       *httptest.Server
	overrides map[string]reply
	seq       map[string][]reply

	mu    sync.Mutex
	reqs  []string
	calls map[string]int
}

func newFakeCloud(overrides map[string]reply, seq map[string][]reply) *fakeCloud {
	f := &fakeCloud{overrides: overrides, seq: seq, calls: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))

	return f
}

func (f *fakeCloud) close() { f.srv.Close() }

// env — адреса фейка для бинарника, кроме unset.
func (f *fakeCloud) env(unset []string) []string {
	u := f.srv.URL

	all := []string{
		"CLOUDRU_ALLOW_HTTP=1",
		"CLOUDRU_IAM_AUTH_URL=" + u + "/iam/token",
		"CLOUDRU_CLICKHOUSE_API_URL=" + u + "/clickhouse",
		"CLOUDRU_REDIS_API_URL=" + u + "/redis",
		"CLOUDRU_KAFKA_API_URL=" + u + "/kafka",
		"CLOUDRU_COMPUTE_API_URL=" + u + "/compute",
		"CLOUDRU_VPC_API_URL=" + u + "/vpc",
		"CLOUDRU_IAM_API_URL=" + u + "/iamapi",
	}

	var out []string

	for _, kv := range all {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(unset, name) {
			out = append(out, kv)
		}
	}

	return out
}

func (f *fakeCloud) serve(w http.ResponseWriter, r *http.Request) {
	// Фейк как HTTPS_PROXY: записываем, куда шёл бинарник, и обрываем туннель.
	if r.Method == http.MethodConnect {
		f.mu.Lock()
		f.reqs = append(f.reqs, "CONNECT "+r.Host)
		f.mu.Unlock()
		w.WriteHeader(http.StatusForbidden)

		return
	}

	raw, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.reqs = append(f.reqs, describe(r, raw))
	n := f.calls[key]
	f.calls[key]++
	f.mu.Unlock()

	rep, ok := f.overrides[key]
	if replies := f.seq[key]; len(replies) > 0 {
		rep, ok = replies[min(n, len(replies)-1)], true
	}

	if !ok {
		rep = defaultReply(r)
	}

	// Каждый выпуск токена — новый токен: в golden видно, с каким из них ушёл повтор после 401.
	if r.URL.Path == "/iam/token" && !ok {
		rep.body = fmt.Sprintf(`{"access_token":"tok%d","expires_in":3600}`, n+1)
	}

	if rep.status == 0 {
		rep.status = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json")

	for k, v := range rep.header {
		w.Header().Set(k, v)
	}

	w.WriteHeader(rep.status)
	_, _ = w.Write([]byte(rep.body))
}

func defaultReply(r *http.Request) reply {
	product, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

	switch {
	case r.URL.Path == "/iam/token":
		return reply{body: `{"access_token":"tok","expires_in":3600}`}
	case r.URL.Path == "/compute/api/v1/availability-zones":
		return reply{body: `[]`}
	case dbaasProducts[product] && strings.Contains(path, "operations/") && r.Method == http.MethodGet:
		return reply{body: opDone}
	case dbaasProducts[product] && r.Method != http.MethodGet:
		return reply{body: opPending}
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/accessKeys"):
		return reply{body: `{"id":"ak1","key_id":"kid2","secret":"s2"}`}
	}

	return reply{body: `{}`}
}

// describe — запрос в устойчивом виде: метод, путь, отсортированный query, заголовки
// User-Agent, Authorization, Content-Type и тело с отсортированными ключами.
func describe(r *http.Request, body []byte) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s %s", r.Method, r.URL.Path)

	if q := r.URL.Query(); len(q) > 0 {
		b.WriteString("?" + q.Encode())
	}

	fmt.Fprintf(&b, "\n  ua: %s", r.Header.Get("User-Agent"))

	if a := r.Header.Get("Authorization"); a != "" {
		fmt.Fprintf(&b, "\n  auth: %s", a)
	}

	if ct := r.Header.Get("Content-Type"); ct != "" {
		fmt.Fprintf(&b, "\n  content-type: %s", ct)
	}

	if len(bytes.TrimSpace(body)) > 0 {
		fmt.Fprintf(&b, "\n  body: %s", canonicalJSON(body))
	}

	return b.String()
}

func canonicalJSON(body []byte) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body) // не JSON (форма токена) — как есть
	}

	out, _ := json.Marshal(v) // json.Marshal сортирует ключи map

	return string(out)
}

func (f *fakeCloud) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.reqs...)
}
