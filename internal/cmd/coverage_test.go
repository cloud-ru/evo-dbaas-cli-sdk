package cmd

// Полнота CLI относительно Public API. CLI гоняется против поддельного облака,
// которое записывает каждый запрос, и записанное сверяется со спецификациями
// api/openapi-*.yaml: каждая операция вызвана, каждое поле тела и параметр
// запроса отправлены, каждое значение перечисления отправлено хотя бы раз.
// Сверяется то, что реально ушло в сеть, а не имена в коде: флаг, который
// принимается, но не передаётся, тест тоже ловит. Процесс — docs/api-coverage.md.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// loadCoverageExceptions — сознательно не покрытое, с причиной: testdata/coverage-exceptions.yaml.
func loadCoverageExceptions(t *testing.T) map[string]string {
	t.Helper()

	raw, err := os.ReadFile("testdata/coverage-exceptions.yaml")
	if err != nil {
		t.Fatal(err)
	}

	m := map[string]string{}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}

	return m
}

// coverageFlagsGlobal — общие флаги корня, к API-полям отношения не имеют.
var coverageFlagsGlobal = map[string]bool{"json": true, "project": true, "api-url": true, "help": true}

// coverageRuns — вызовы CLI, которыми тест обходит API. Новый флаг или команда
// без строки здесь роняет тест (каждый флаг каждой команды обязан встретиться).
var coverageRuns = []covRun{
	// ClickHouse
	{args: "clickhouse versions list"},
	{args: "clickhouse specs list --version 25.8 --zone zone-1 --role server"},
	{args: "clickhouse specs list --zone zone-1 --role keeper"},
	{args: "clickhouse clusters list"},
	{args: "clickhouse clusters get c1"},
	{args: "clickhouse clusters create --name n --description d --version 25.8 --spec s --shards 1 --replicas 1 --storage-gib 40 --subnet-id sn1 --keeper embedded"},
	{args: "clickhouse clusters create --name n --version 25.8 --spec s --shards 1 --replicas 3 --subnet-id sn1 --subnet-id sn2 --keeper standalone --keeper-spec ks --keeper-storage-gib 10 --config max_connections=100 --config max_concurrent_queries=50 --config background_pool_size=1 --config background_schedule_pool_size=1 --config keep_alive_timeout=1 --config mark_cache_size=1 --config max_partition_size_to_drop=1 --config max_session_timeout=1 --config max_table_size_to_drop=1 --config uncompressed_cache_size=1 --logging --log-group-id lg --body {\"description\":\"из-body\"}"},
	{args: "clickhouse clusters resize c1 --spec s2 --storage-gib 80"},
	{args: "clickhouse clusters resize c1 --keeper-spec ks2 --keeper-storage-gib 20 --config max_connections=200 --body {}"},
	{args: "clickhouse databases get c1 db"},
	{args: "clickhouse users get c1 u"},
	{args: "clickhouse users update c1 u --password-env PW", env: "PW=Secret-2"},
	{args: "clickhouse users update c1 u --role r1"},
	{args: "clickhouse users update c1 u --password-stdin", stdin: "Secret-2\n"},
	{args: "clickhouse clusters delete c1 --yes"},
	{args: "clickhouse databases list c1"},
	{args: "clickhouse databases create c1 db"},
	{args: "clickhouse databases delete c1 db --yes"},
	{args: "clickhouse users list c1"},
	{args: "clickhouse users create c1 u --password-env PW --role r1", env: "PW=Secret-1"},
	{args: "clickhouse users create c1 u --password-stdin", stdin: "Secret-1\n"},
	{args: "clickhouse users delete c1 u --yes"},
	{args: "clickhouse users grant c1 u r1"},
	{args: "clickhouse users revoke c1 u r1"},
	{args: "clickhouse roles list c1"},
	{args: "clickhouse operations get op1"},
	{args: "clickhouse operations list c1"},
	// Redis
	{args: "redis versions list"},
	{args: "redis specs list --version-id v1 --zone zone-1"},
	{args: "redis clusters list"},
	{args: "redis clusters get c1"},
	{args: "redis clusters create --name n --description d --version-id v1 --spec s --type standalone --shards 1 --replicas 0 --storage-gib 15 --subnet-id sn1"},
	{args: "redis clusters create --name n --version-id v1 --spec s --type master-replica --replicas 1 --subnet-id sn1"},
	{args: "redis clusters create --name n --version-id v1 --spec s --subnet-id sn1 --param maxmemory-policy=allkeys-lru --logging --log-group-id lg --body {}"},
	{args: "redis clusters resize c1 --storage-gib 30"},
	{args: "redis clusters resize c1 --spec s2"},
	{args: "redis clusters resize c1 --param maxmemory-policy=noeviction --body {}"},
	{args: "redis specs list --cluster-id c1"},
	{args: "redis users get c1 u"},
	{args: "redis users update c1 u --password-env PW --role r1", env: "PW=Secret-2"},
	{args: "redis users update c1 u --password-stdin", stdin: "Secret-2\n"},
	{args: "redis clusters delete c1 --yes"},
	{args: "redis users list c1"},
	{args: "redis users create c1 u --password-env PW --role r1", env: "PW=Secret-1"},
	{args: "redis users create c1 u --password-stdin", stdin: "Secret-1\n"},
	{args: "redis users delete c1 u --yes"},
	{args: "redis roles list c1"},
	{args: "redis operations get op1"},
	{args: "redis operations list c1"},
	// Kafka
	{args: "kafka versions list"},
	{args: "kafka specs list --version-id v1 --zone zone-1"},
	{args: "kafka clusters list"},
	{args: "kafka clusters get c1"},
	{args: "kafka clusters create --name n --description d --version-id v1 --spec s --brokers 3 --storage-gib 40 --subnet-id sn1 --ui --cruise-control --auto-rebalance"},
	{args: "kafka clusters create --name n --version-id v1 --spec s --storage-gib 40 --subnet-id sn1 --param auto.create.topics.enable=false --logging --log-group-id lg --body {}"},
	{args: "kafka clusters resize c1 --spec s2 --storage-gib 80"},
	{args: "kafka clusters resize c1 --param auto.create.topics.enable=false --body {}"},
	{args: "kafka specs list --cluster-id c1"},
	{args: "kafka topics get c1 t"},
	{args: "kafka topics update c1 t --partitions 6 --replication 3 --param retention.ms=2"},
	{args: "kafka users get c1 u"},
	{args: "kafka users update c1 u --password-env PW --role r1", env: "PW=Secret-2"},
	{args: "kafka users update c1 u --password-stdin", stdin: "Secret-2\n"},
	{args: "kafka clusters delete c1 --yes"},
	{args: "kafka topics list c1 --filter t"},
	{args: "kafka topics create c1 t --partitions 3 --replication 3 --param retention.ms=1"},
	{args: "kafka topics delete c1 t --yes"},
	{args: "kafka users list c1"},
	{args: "kafka users create c1 u --password-env PW --role r1", env: "PW=Secret-1"},
	{args: "kafka users create c1 u --password-stdin", stdin: "Secret-1\n"},
	{args: "kafka users delete c1 u --yes"},
	{args: "kafka roles list c1"},
	{args: "kafka operations get op1"},
	{args: "kafka operations list c1"},
	// Общее
	{args: "zones list"},
	// Сеть
	{args: "network subnets list --zone " + testZone + " --vpc-id " + testVPC + " --name sn"},
	{args: "network subnets get " + testSubnet},
	{args: "network subnets create --name sn --zone " + testZone + " --cidr 10.0.9.0/24"},
	{args: "network subnets create --name sn --zone " + testZone + " --cidr 10.0.9.0/24 --vpc-id " + testVPC + " --description d --gateway 10.0.9.1 --dns 8.8.8.8 --isolated"},
	{args: "network subnets delete " + testSubnet + " --yes"},
	{args: "network vpcs list"},
	{args: "network vpcs get v1"},
	{args: "network vpcs create --name v --description d"},
	{args: "network vpcs delete v1 --yes"},
	// IAM
	{args: "iam service-accounts list"},
	{args: "iam service-accounts get sa1"},
	{args: "iam service-accounts create --name a --description d"},
	{args: "iam service-accounts delete sa1 --yes"},
	{args: "iam access-keys list sa1"},
	{args: "iam access-keys get sa1 ak1"},
	{args: "iam access-keys create sa1 --env-file ENVFILE --description d --ttl 720h"},
	{args: "iam access-keys delete sa1 ak1 --yes"},
	{args: "iam roles list --subject sa1"},
	{args: "iam roles get p1"},
	{args: "iam roles grant --subject sa1 --role dbaas.clickhouse.admin --expires 2026-12-31T00:00:00Z"},
	{args: "iam roles revoke p1 --yes"},
}

type covRun struct {
	args  string
	stdin string
	env   string // ИМЯ=значение
}

type covRequest struct {
	product, method, path string
	query                 map[string][]string
	body                  []byte
}

// covRecorder — облако, которое на всё отвечает 200 и записывает запросы.
type covRecorder struct {
	mu   sync.Mutex
	reqs []covRequest
}

func (rec *covRecorder) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.URL.Path {
	case "/iam/token":
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
		return
	case "/compute/api/v1/availability-zones":
		_, _ = w.Write([]byte(`[]`))
		return
	}

	product, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	raw, _ := io.ReadAll(r.Body)

	rec.mu.Lock()
	rec.reqs = append(rec.reqs, covRequest{product: product, method: r.Method, path: "/" + path, query: r.URL.Query(), body: raw})
	rec.mu.Unlock()

	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/accessKeys") {
		_, _ = w.Write([]byte(`{"id":"ak1","key_id":"kid2","secret":"s2"}`))
		return
	}

	_, _ = w.Write([]byte(`{}`))
}

func TestAPICoverage(t *testing.T) {
	coverageExceptions := loadCoverageExceptions(t)

	rec := &covRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(rec.serve))
	t.Cleanup(srv.Close)

	pollDelay = time.Millisecond

	t.Setenv("CLOUDRU_IAM_AUTH_URL", srv.URL+"/iam/token")
	t.Setenv("CLOUDRU_CLICKHOUSE_API_URL", srv.URL+"/clickhouse")
	t.Setenv("CLOUDRU_REDIS_API_URL", srv.URL+"/redis")
	t.Setenv("CLOUDRU_KAFKA_API_URL", srv.URL+"/kafka")
	t.Setenv("CLOUDRU_COMPUTE_API_URL", srv.URL+"/compute")
	t.Setenv("CLOUDRU_VPC_API_URL", srv.URL+"/vpc")
	t.Setenv("CLOUDRU_IAM_API_URL", srv.URL+"/iamapi")
	t.Setenv("CLOUDRU_KEY_ID", "kid")
	t.Setenv("CLOUDRU_KEY_SECRET", "sec")
	t.Setenv("CLOUDRU_PROJECT_ID", testProject)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	usedFlags := map[string]bool{} // "redis users create --role"
	for _, run := range coverageRuns {
		args := strings.Fields(strings.ReplaceAll(run.args, "ENVFILE", filepath.Join(t.TempDir(), "key.env")))
		if run.env != "" {
			k, v, _ := strings.Cut(run.env, "=")
			t.Setenv(k, v)
		}

		root := NewDBaaSRoot()
		root.SetIn(strings.NewReader(run.stdin))

		var out, errOut bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(args)

		if err := root.Execute(); err != nil {
			t.Errorf("%s: %v\n%s", run.args, err, errOut.String())
			continue
		}

		cmd, _, _ := root.Find(args)
		cmd.Flags().Visit(func(f *pflag.Flag) { usedFlags[cmd.CommandPath()+" --"+f.Name] = true })
	}

	var missing []string

	miss := func(key string) {
		if _, ok := coverageExceptions[key]; ok {
			return // пустую причину ловит проверка ниже
		}

		missing = append(missing, key)
	}

	// Каждый флаг каждой команды встречается в coverageRuns — иначе обход неполон.
	walkCommands(NewDBaaSRoot(), func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if coverageFlagsGlobal[f.Name] {
				return
			}

			if _, ok := coverageExceptions["flag --"+f.Name]; ok {
				return
			}

			if !usedFlags[c.CommandPath()+" --"+f.Name] {
				miss("run " + c.CommandPath() + " --" + f.Name)
			}
		})
	})

	for _, p := range []struct {
		name, file string
		scope      []string // только эти пути; пусто — вся спецификация
	}{
		{"clickhouse", "openapi-clickhouse.yaml", nil},
		{"redis", "openapi-redis.yaml", nil},
		{"kafka", "openapi-kafka.yaml", nil},
		// Из API виртуальных машин, VPC и IAM CLI берёт то, что нужно для
		// кластера: подсети, VPC, сервисные аккаунты, ключи, роли. Внутри
		// этих путей покрытие полное, как у продуктов.
		{"compute", "openapi-compute.yaml", []string{"/api/v1/subnets", "/api/v1/subnets/{subnet_id}"}},
		{"vpc", "openapi-vpc.yaml", []string{"/v1/vpcs", "/v1/vpcs/{vpcId}", "/v1/vpcs/operations/{operationId}"}},
		{"iamapi", "openapi-iam.yaml", []string{
			"/api/v2/serviceAccounts", "/api/v2/serviceAccounts/{serviceAccountId}",
			"/api/v2/serviceAccounts/{serviceAccountId}/accessKeys",
			"/api/v2/serviceAccounts/{serviceAccountId}/accessKeys/{accessKeyId}",
			"/api/v3/permissions", "/api/v3/permissions/{permissionId}",
		}},
	} {
		spec := loadSpec(t, "../../api/"+p.file)
		for _, op := range spec.operations() {
			if len(p.scope) > 0 && !slices.Contains(p.scope, op.path) {
				continue
			}

			key := p.name + " " + op.method + " " + op.path

			var hits []covRequest
			for _, r := range rec.reqs {
				if r.product == p.name && r.method == op.method && op.re.MatchString(r.path) {
					hits = append(hits, r)
				}
			}

			if len(hits) == 0 {
				miss("op " + key)
				continue
			}

			for _, q := range op.query {
				if q.deprecated {
					continue
				}

				sent := map[string]bool{}
				for _, r := range hits {
					for _, v := range r.query[q.name] {
						sent[v] = true
					}
				}

				if len(sent) == 0 {
					miss("query " + key + " " + q.name)
					continue
				}

				for _, e := range q.enum {
					if !sent[e] && !strings.HasSuffix(e, "_UNSPECIFIED") {
						miss("enum " + key + " " + q.name + "=" + e)
					}
				}
			}

			if op.body == nil {
				continue
			}

			sent := map[string]map[string]bool{} // путь поля → отправленные значения
			for _, r := range hits {
				var v any
				if json.Unmarshal(r.body, &v) == nil {
					collectJSON("", v, sent)
				}
			}

			for _, f := range spec.fields("", op.body) {
				if sent[f.path] == nil {
					miss("field " + key + " " + f.path)
					continue
				}

				for _, e := range f.enum {
					if !sent[f.path][e] && !strings.HasSuffix(e, "_UNSPECIFIED") {
						miss("enum " + key + " " + f.path + "=" + e)
					}
				}
			}
		}
	}

	for key, reason := range coverageExceptions {
		if strings.TrimSpace(reason) == "" {
			missing = append(missing, key+" (исключение без причины)")
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("не покрыто относительно API (%d) — добавьте команду, флаг или исключение с причиной, см. docs/api-coverage.md:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func walkCommands(c *cobra.Command, fn func(*cobra.Command)) {
	if c.Runnable() {
		fn(c)
	}

	for _, s := range c.Commands() {
		walkCommands(s, fn)
	}
}

// collectJSON раскладывает тело запроса в пути полей: объект → «a.b»,
// массив — по элементам под тем же путём, листья — со значением.
func collectJSON(prefix string, v any, out map[string]map[string]bool) {
	mark := func(val string) {
		if out[prefix] == nil {
			out[prefix] = map[string]bool{}
		}

		if val != "" {
			out[prefix][val] = true
		}
	}
	switch x := v.(type) {
	case map[string]any:
		if prefix != "" {
			mark("")
		}

		for k, val := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}

			collectJSON(p, val, out)
		}
	case []any:
		mark("")

		for _, e := range x {
			collectJSON(prefix, e, out)
		}
	case string:
		mark(x)
	default:
		mark(fmt.Sprint(x))
	}
}

// --- спецификация ---

type covSpec struct{ doc map[string]any }

type covOp struct {
	method, path string
	re           *regexp.Regexp
	query        []covParam
	body         map[string]any
}

type covParam struct {
	name       string
	deprecated bool
	enum       []string
}

type covField struct {
	path string
	enum []string
}

func loadSpec(t *testing.T, file string) covSpec {
	t.Helper()

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	return covSpec{doc}
}

func (s covSpec) resolve(m map[string]any) map[string]any {
	for m != nil {
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}

		var cur any = s.doc
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			cur = cur.(map[string]any)[part]
		}

		m, _ = cur.(map[string]any)
	}

	return m
}

func (s covSpec) operations() []covOp {
	var ops []covOp

	paths, _ := s.doc["paths"].(map[string]any)
	for path, item := range paths {
		methods, _ := item.(map[string]any)
		for method, raw := range methods {
			op, ok := raw.(map[string]any)
			if !ok || method == "parameters" {
				continue
			}

			pat := regexp.QuoteMeta(path)
			pat = regexp.MustCompile(`\\\{[^}]+\\\}`).ReplaceAllString(pat, `[^/]+`)
			o := covOp{method: strings.ToUpper(method), path: path, re: regexp.MustCompile("^" + pat + "$")}

			params, _ := op["parameters"].([]any)
			for _, p := range params {
				pm := s.resolve(p.(map[string]any))
				if pm["in"] == "body" { // Swagger 2.0 (IAM): тело — параметр
					o.body = s.resolve(asMap(pm["schema"]))
					continue
				}

				if pm["in"] != "query" {
					continue
				}

				schema := s.resolve(asMap(pm["schema"]))
				if schema == nil { // Swagger 2.0: тип и enum — у самого параметра
					schema = pm
				}

				o.query = append(o.query, covParam{
					name:       pm["name"].(string),
					deprecated: pm["deprecated"] == true || schema["deprecated"] == true,
					enum:       enumOf(schema),
				})
			}

			if rb := s.resolve(asMap(op["requestBody"])); rb != nil {
				content := asMap(rb["content"])
				o.body = s.resolve(asMap(asMap(content["application/json"])["schema"]))
			}

			ops = append(ops, o)
		}
	}

	return ops
}

// fields — пути всех не устаревших полей схемы, вглубь объектов и массивов.
// Поле-словарь (additionalProperties) считается листом.
func (s covSpec) fields(prefix string, schema map[string]any) []covField {
	var out []covField

	props := asMap(schema["properties"])
	for name, raw := range props {
		ps := s.resolve(asMap(raw))
		if ps["deprecated"] == true {
			continue
		}

		p := name
		if prefix != "" {
			p = prefix + "." + name
		}

		item := ps
		if ps["type"] == "array" {
			item = s.resolve(asMap(ps["items"]))
		}

		out = append(out, covField{path: p, enum: enumOf(item)})
		if asMap(item["properties"]) != nil {
			out = append(out, s.fields(p, item)...)
		}
	}

	return out
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func enumOf(schema map[string]any) []string {
	var out []string

	vals, _ := schema["enum"].([]any)
	for _, v := range vals {
		out = append(out, fmt.Sprint(v))
	}

	return out
}

// TestSkillCommandsParse — каждая команда из блоков bash в skills/*/SKILL.md
// и skills/*/references/*.md
// разбирается CLI: существует, флаги известны. Скилл, советующий несуществующий
// флаг, ломает агента так же, как недостающий флаг.
func TestSkillCommandsParse(t *testing.T) {
	files, _ := filepath.Glob("../../skills/*/SKILL.md")
	refs, _ := filepath.Glob("../../skills/*/references/*.md")

	files = append(files, refs...)
	if len(files) == 0 {
		t.Fatal("скиллы не найдены")
	}

	placeholder := regexp.MustCompile(`<[^>]*>`)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		inBash := false

		var lines []string
		for _, l := range strings.Split(string(raw), "\n") {
			switch {
			case strings.HasPrefix(l, "```bash"):
				inBash = true
			case strings.HasPrefix(l, "```"):
				inBash = false
			case inBash:
				lines = append(lines, l)
			}
		}

		joined := strings.ReplaceAll(strings.Join(lines, "\n"), "\\\n", " ")
		for _, l := range strings.Split(joined, "\n") {
			if i := strings.Index(l, " #"); i >= 0 {
				l = l[:i]
			}

			for _, sep := range []string{"||", "&&", "|", ";"} {
				if i := strings.Index(l, sep); i >= 0 {
					l = l[:i]
				}
			}

			l = placeholder.ReplaceAllString(l, "1") // число годится и строковому флагу

			fields := strings.Fields(l)
			for len(fields) > 0 && strings.Contains(fields[0], "=") { // VAR='…' перед командой
				fields = fields[1:]
			}

			if len(fields) == 0 {
				continue
			}

			var root *cobra.Command
			switch fields[0] {
			case "cloudru-dbaas":
				root = NewDBaaSRoot()
			case "ch-cloudru":
				t.Errorf("%s: %q: бинарник ch-cloudru убран, пиши cloudru-dbaas clickhouse", file, l)

				continue
			default:
				continue
			}

			root.InitDefaultVersionFlag() // cobra добавляет --version только в Execute

			cmd, rest, err := root.Find(fields[1:])
			if err == nil {
				err = cmd.ParseFlags(rest)
			}

			if err == nil && !cmd.Runnable() && cmd != root {
				err = fmt.Errorf("не команда")
			}

			if err != nil {
				t.Errorf("%s: %q: %v", file, strings.Join(fields, " "), err)
			}
		}
	}
}
