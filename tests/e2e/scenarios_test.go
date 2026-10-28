package e2e_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const gexecTimeout = 10 * time.Second

// commandRuns — хотя бы один вызов на каждую листовую команду (проверяет «каждая команда
// в сетке»). Перенесено из coverageRuns старого internal/cli (удалён на шаге 7).

// Идентификаторы compute — UUID (CLI проверяет до запроса): проект, подсеть, зона, VPC.
const (
	cProj = "--project 11111111-1111-4111-8111-111111111111"
	cSN   = "22222222-2222-4222-8222-222222222222"
	cZone = "33333333-3333-4333-8333-333333333333"
	cVPC  = "44444444-4444-4444-8444-444444444444"
)

var commandRuns = []scenario{
	// ClickHouse
	{args: "clickhouse auth check"},
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
	{args: "clickhouse users update c1 u --password-env PW", env: []string{"PW=Secret-2"}},
	{args: "clickhouse users update c1 u --role r1"},
	{args: "clickhouse users update c1 u --password-stdin", stdin: "Secret-2\n"},
	{args: "clickhouse clusters delete c1 --yes"},
	{args: "clickhouse databases list c1"},
	{args: "clickhouse databases create c1 db"},
	{args: "clickhouse databases delete c1 db --yes"},
	{args: "clickhouse users list c1"},
	{args: "clickhouse users create c1 u --password-env PW --role r1", env: []string{"PW=Secret-1"}},
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
	{args: "redis users update c1 u --password-env PW --role r1", env: []string{"PW=Secret-2"}},
	{args: "redis users update c1 u --password-stdin", stdin: "Secret-2\n"},
	{args: "redis clusters delete c1 --yes"},
	{args: "redis users list c1"},
	{args: "redis users create c1 u --password-env PW --role r1", env: []string{"PW=Secret-1"}},
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
	{args: "kafka users update c1 u --password-env PW --role r1", env: []string{"PW=Secret-2"}},
	{args: "kafka users update c1 u --password-stdin", stdin: "Secret-2\n"},
	{args: "kafka clusters delete c1 --yes"},
	{args: "kafka topics list c1 --filter t"},
	{args: "kafka topics create c1 t --partitions 3 --replication 3 --param retention.ms=1"},
	{args: "kafka topics delete c1 t --yes"},
	{args: "kafka users list c1"},
	{args: "kafka users create c1 u --password-env PW --role r1", env: []string{"PW=Secret-1"}},
	{args: "kafka users create c1 u --password-stdin", stdin: "Secret-1\n"},
	{args: "kafka users delete c1 u --yes"},
	{args: "kafka roles list c1"},
	{args: "kafka operations get op1"},
	{args: "kafka operations list c1"},
	// Общее
	{args: "zones list"},
	// Сеть
	{args: "network subnets list --zone " + cZone + " --vpc-id " + cVPC + " --name sn " + cProj},
	{args: "network subnets get " + cSN},
	{args: "network subnets create --name sn --zone " + cZone + " --cidr 10.0.9.0/24 " + cProj},
	{args: "network subnets create --name sn --zone " + cZone + " --cidr 10.0.9.0/24 --vpc-id " + cVPC + " --description d --gateway 10.0.9.1 --dns 8.8.8.8 --isolated " + cProj},
	{args: "network subnets delete " + cSN + " --yes"},
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

// edgeRuns — поведение, которое рефакторинг шагов 3–7 трогает: ошибки ключа и API,
// коды выхода, ожидание операций, подтверждение удаления, --json.
var edgeRuns = []scenario{
	{name: "нет ключа", args: "redis clusters list", noKey: true},
	{name: "ключ отклонён 401", args: "redis clusters list", overrides: map[string]reply{
		"POST /iam/token": {status: 401, body: `{"error":"invalid_client"}`},
	}},
	{name: "api 403", args: "clickhouse clusters list", overrides: map[string]reply{
		"GET /clickhouse/v1/clusters": {status: 403, body: `{"code":7,"message":"permission denied"}`},
	}},
	{name: "api 404", args: "kafka clusters get c404", overrides: map[string]reply{
		"GET /kafka/v1/clusters/c404": {status: 404, body: `{"code":5,"message":"cluster not found"}`},
	}},
	// GET с 429 и 5xx повторяется с паузой — для сетки берём мутации, их не повторяют.
	{name: "api 429", args: "redis clusters delete c1 --yes", overrides: map[string]reply{
		"DELETE /redis/v1/clusters/c1": {status: 429, body: `{"code":8,"message":"rate limit"}`},
	}},
	// Шлюз кладёт причину в grpc-message, тело пустое.
	{name: "api 400 без тела", args: "network vpcs create --name v", overrides: map[string]reply{
		"POST /vpc/v1/vpcs": {status: 400, header: map[string]string{"Grpc-Message": "name%20is%20invalid"}},
	}},
	{name: "api 500", args: "network vpcs create --name v", overrides: map[string]reply{
		"POST /vpc/v1/vpcs": {status: 500, body: `{"message":"internal"}`},
	}},
	// compute проверяет UUID, roles grant — RFC 3339 до запроса (меняет поведение).
	{name: "подсеть не UUID", args: "network subnets get sn1"},
	{name: "проект compute не UUID", args: "network subnets list --project proj-1"},
	{name: "срок роли не RFC 3339", args: "iam roles grant --subject sa1 --role r --expires 2026-12-31"},
	{name: "wait успех", args: "redis clusters delete c1 --yes --wait"},
	{name: "wait успех json", args: "redis clusters delete c1 --yes --wait --json"},
	{name: "wait операция с ошибкой", args: "kafka clusters delete c1 --yes --wait", overrides: map[string]reply{
		"GET /kafka/v1/operations/op1": {body: `{"id":"op1","done":true,"resource_id":"c1","error":{"code":9,"message":"кластер занят"}}`},
	}},
	{name: "wait опрос 404", args: "clickhouse clusters delete c1 --yes --wait", overrides: map[string]reply{
		"GET /clickhouse/v1/operations/op1": {status: 404, body: `{"code":5,"message":"operation not found"}`},
	}},
	// Первый опрос сразу, следующий через 5 с — таймаут 1 с даёт ровно один опрос.
	{name: "wait таймаут", args: "redis clusters delete c1 --yes --wait --timeout 1s", overrides: map[string]reply{
		"GET /redis/v1/operations/op1": {body: `{"id":"op1","done":false,"resource_id":"c1"}`},
	}},
	// Ctrl-C во время --wait — код 2 и подсказка, как дождаться.
	{name: "wait ctrl-c", args: "redis clusters delete c1 --yes --wait", interrupt: "Удаление кластера", overrides: map[string]reply{
		"GET /redis/v1/operations/op1": {body: `{"id":"op1","done":false,"description":"Удаление кластера"}`},
	}},
	// --wait при ответе без операции не выходит молча с кодом 0.
	{name: "wait ответ без операции", args: "redis clusters delete c1 --yes --wait", overrides: map[string]reply{
		"DELETE /redis/v1/clusters/c1": {body: `{}`},
	}},
	{name: "ответ redis get", args: "redis clusters get r9", overrides: map[string]reply{
		"GET /redis/v1/clusters/r9": {body: `{"id":"r9","name":"cache","status":"READY","type":"STANDALONE","shards":1,"replicas":0,"specification_id":"s","version_id":"v","redis_hosts":["10.0.1.7"]}`},
	}},
	{name: "ответ redis list", args: "redis clusters list", overrides: map[string]reply{
		"GET /redis/v1/clusters": {body: `{"clusters":[{"id":"r1","name":"cache","status":"READY","type":"STANDALONE","shards":1,"replicas":0,"specification_id":"s","version_id":"v"}]}`},
	}},
	{name: "ответ kafka get", args: "kafka clusters get k9", overrides: map[string]reply{
		"GET /kafka/v1/clusters/k9": {body: `{"id":"k9","name":"events","status":"READY","brokers":3,"storage_gib":40,"specification_id":"s","version_id":"v","kafka_hosts":["10.0.1.8:9092"],"kafka_ui_host":"10.0.1.9"}`},
	}},
	{name: "ответ kafka list", args: "kafka clusters list", overrides: map[string]reply{
		"GET /kafka/v1/clusters": {body: `{"clusters":[{"id":"k2","name":"bus2","brokers":1,"storage_gib":40,"specification_id":"s","version_id":"v"}]}`},
	}},
	{name: "ответ clickhouse get", args: "clickhouse clusters get c-new", overrides: map[string]reply{
		"GET /clickhouse/v1/clusters/c-new": {body: `{"id":"c-new","name":"demo","status":"READY","version":"24.3","shards":1,"replicas":0,"specification_id":"s1","project_id":"proj-1","hosts":["h1:9440"]}`},
	}},
	{name: "ответ clickhouse list json", args: "clickhouse clusters list --json", overrides: map[string]reply{
		"GET /clickhouse/v1/clusters": {body: `{"clusters":[{"id":"c1","name":"n","new_field":{"x":12345678901234567890}}]}`},
	}},
	{name: "удаление без yes", args: "redis clusters delete c1"},
	{name: "удаление без yes json", args: "kafka topics delete c1 t --json"},
	{name: "список json", args: "kafka clusters list --json"},
	{name: "неизвестная команда", args: "redis clusters frobnicate"},
	{name: "неизвестный флаг", args: "redis clusters list --frob"},
	{name: "нет аргумента", args: "redis clusters get"},
	{name: "версия", args: "--version"},
	{name: "помощь", args: "--help"},
	// Язык по LANG: переведённые тексты zones — из каталога ru, остальное пока как есть.
	{name: "помощь zones ru", args: "zones --help", env: []string{"LANG=ru_RU.UTF-8"}},
	{name: "помощь zones en", args: "zones --help", env: []string{"LANG=ru_RU.UTF-8", "LC_ALL=C"}},
	// Транспорт: повтор GET и перевыпуск токена. Пауза перед повтором GET — 2 с.
	{name: "повтор get 500", args: "redis clusters list", seq: map[string][]reply{
		"GET /redis/v1/clusters": {{status: 500, body: `{"message":"internal"}`}, {body: `{"clusters":[]}`}},
	}},
	{name: "повтор get 403 проверки доступа", args: "clickhouse clusters list", seq: map[string][]reply{
		"GET /clickhouse/v1/clusters": {{status: 403, body: `{"code":7,"message":"failed to verify project access"}`}, {body: `{"clusters":[]}`}},
	}},
	{name: "api 401 перевыпуск токена", args: "kafka clusters list", seq: map[string][]reply{
		"GET /kafka/v1/clusters": {{status: 401, body: `{"code":16,"message":"unauthenticated"}`}, {body: `{"clusters":[]}`}},
	}},
	// Ожидание: каждый путь --wait.
	{name: "wait создание clickhouse", args: "clickhouse clusters create --name n --version 25.8 --spec s --shards 1 --replicas 1 --subnet-id sn1 --keeper embedded --wait"},
	{name: "wait создание redis", args: "redis clusters create --name n --version-id v1 --spec s --subnet-id sn1 --wait"},
	{name: "wait создание kafka", args: "kafka clusters create --name n --version-id v1 --spec s --storage-gib 40 --subnet-id sn1 --wait"},
	{name: "wait operations get", args: "redis operations get op1 --wait", seq: map[string][]reply{
		"GET /redis/v1/operations/op1": {{body: opPending}, {body: opDone}},
	}},
	{
		name: "wait создание подсети", args: "network subnets create --name sn --zone " + cZone + " --cidr 10.0.9.0/24 --wait " + cProj,
		overrides: map[string]reply{"POST /compute/api/v1/subnets": {body: `{"id":"` + cSN + `","state":"creating"}`}},
		seq: map[string][]reply{"GET /compute/api/v1/subnets/" + cSN: {
			{body: `{"id":"` + cSN + `","state":"creating"}`}, {body: `{"id":"` + cSN + `","state":"created"}`},
		}},
	},
	{name: "wait удаление подсети", args: "network subnets delete " + cSN + " --yes --wait", overrides: map[string]reply{
		"GET /compute/api/v1/subnets/" + cSN: {status: 404, body: `{"message":"not found"}`},
	}},
	{name: "wait удаление подсети таймаут", args: "network subnets delete " + cSN + " --yes --wait --timeout 1s", overrides: map[string]reply{
		"GET /compute/api/v1/subnets/" + cSN: {body: `{"id":"` + cSN + `","state":"deleting"}`},
	}},
	{
		name: "wait создание vpc", args: "network vpcs create --name v --wait",
		overrides: map[string]reply{"POST /vpc/v1/vpcs": {body: `{"id":"vop1","done":false}`}},
		seq: map[string][]reply{"GET /vpc/v1/vpcs/operations/vop1": {
			{body: `{"id":"vop1","done":false}`}, {body: `{"id":"vop1","done":true,"resourceId":"v1"}`},
		}},
	},
	{name: "wait операция vpc с ошибкой", args: "network vpcs create --name v --wait", overrides: map[string]reply{
		"POST /vpc/v1/vpcs":                {body: `{"id":"vop1","done":false}`},
		"GET /vpc/v1/vpcs/operations/vop1": {body: `{"id":"vop1","done":true,"error":{"message":"квота исчерпана"}}`},
	}},
	// error без message — тоже ошибка операции.
	{name: "wait операция vpc с ошибкой без описания", args: "network vpcs create --name v --wait", overrides: map[string]reply{
		"POST /vpc/v1/vpcs":                {body: `{"id":"vop1","done":false}`},
		"GET /vpc/v1/vpcs/operations/vop1": {body: `{"id":"vop1","done":true,"error":{"code":8}}`},
	}},
	{name: "wait vpc таймаут", args: "network vpcs delete v1 --yes --wait --timeout 1s", overrides: map[string]reply{
		"DELETE /vpc/v1/vpcs/v1":           {body: `{"id":"vop1","done":false}`},
		"GET /vpc/v1/vpcs/operations/vop1": {body: `{"id":"vop1","done":false}`},
	}},
	{name: "wait удаление vpc", args: "network vpcs delete v1 --yes --wait", overrides: map[string]reply{
		"DELETE /vpc/v1/vpcs/v1":           {body: `{"id":"vop1","done":false}`},
		"GET /vpc/v1/vpcs/operations/vop1": {body: `{"id":"vop1","done":true,"resourceId":"v1"}`},
	}},
	// Адреса API по умолчанию: у продуктов DBaaS видны в --help, у остальных — хост CONNECT через прокси.
	{name: "адрес по умолчанию clickhouse", args: "clickhouse clusters --help", unset: []string{"CLOUDRU_CLICKHOUSE_API_URL"}},
	{name: "адрес по умолчанию redis", args: "redis clusters --help", unset: []string{"CLOUDRU_REDIS_API_URL"}},
	{name: "адрес по умолчанию kafka", args: "kafka clusters --help", unset: []string{"CLOUDRU_KAFKA_API_URL"}},
	{name: "адрес по умолчанию compute", args: "network subnets delete " + cSN + " --yes", proxy: true, unset: []string{"CLOUDRU_COMPUTE_API_URL"}},
	{name: "адрес по умолчанию vpc", args: "network vpcs delete v1 --yes", proxy: true, unset: []string{"CLOUDRU_VPC_API_URL"}},
	{name: "адрес по умолчанию iam api", args: "iam service-accounts delete sa1 --yes", proxy: true, unset: []string{"CLOUDRU_IAM_API_URL"}},
	{name: "адрес по умолчанию iam токен", args: "redis clusters list", proxy: true, unset: []string{"CLOUDRU_IAM_AUTH_URL"}},
	// Флаг --project сильнее CLOUDRU_PROJECT_ID.
	{name: "project флаг сильнее env", args: "redis clusters list --project p2"},
}

// noYesRuns — отказ без --yes для каждой команды удаления из commandRuns.
func noYesRuns() []scenario {
	var out []scenario

	for _, s := range commandRuns {
		if args := strings.ReplaceAll(s.args, " --yes", ""); args != s.args {
			out = append(out, scenario{name: "без yes " + args, args: args})
		}
	}

	return out
}

// allRuns — все сценарии; name — имя golden-файла.
func allRuns() []scenario {
	var out []scenario

	for _, s := range commandRuns {
		s.name = s.args
		out = append(out, s)
	}

	for _, s := range append(append([]scenario(nil), edgeRuns...), noYesRuns()...) {
		s.name = "edge " + s.name
		out = append(out, s)
	}

	return out
}

var _ = Describe("Сценарии", func() {
	for _, s := range allRuns() {
		It(s.name, func() {
			matchGolden(s.name, runScenario(s))
		})
	}
})

var _ = Describe("Сетка", func() {
	It("у каждого golden есть сценарий, имена не совпадают", func() {
		want := map[string]string{}

		for _, s := range allRuns() {
			g := goldenName(s.name)
			Expect(want).NotTo(HaveKey(g), "сценарии %q и %q пишут в один golden", want[g], s.name)
			want[g] = s.name
		}

		files, err := filepath.Glob(filepath.Join("testdata", "golden", "*.golden"))
		Expect(err).NotTo(HaveOccurred())

		for _, f := range files {
			Expect(want).To(HaveKey(strings.TrimSuffix(filepath.Base(f), ".golden")),
				"golden %s без сценария: сценарий удалён молча", f)
		}
	})

	It("покрывает каждую листовую команду", func() {
		covered := map[string]bool{}

		for _, s := range commandRuns {
			covered[commandPath(s.args)] = true
		}

		for _, leaf := range leafCommands(nil) {
			Expect(covered).To(HaveKey(strings.Join(leaf, " ")), "команда без сценария в commandRuns")
		}
	})
})

// commandPath — слова до первого флага или аргумента-идентификатора: команду знает --help.
func commandPath(args string) string {
	words := strings.Fields(args)
	for n := len(words); n > 0; n-- {
		if isLeaf(words[:n]) {
			return strings.Join(words[:n], " ")
		}
	}

	return args
}

var leaves map[string]bool

func isLeaf(path []string) bool {
	if leaves == nil {
		leaves = map[string]bool{}
		for _, l := range leafCommands(nil) {
			leaves[strings.Join(l, " ")] = true
		}
	}

	return leaves[strings.Join(path, " ")]
}

// leafCommands обходит дерево команд по --help бинарника.
func leafCommands(prefix []string) [][]string {
	args := append(append([]string(nil), prefix...), "--help")

	out, err := exec.Command(binPath, args...).Output()
	Expect(err).NotTo(HaveOccurred())

	var subs []string

	inList := false
	for line := range strings.SplitSeq(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "Available Commands:"):
			inList = true
		case inList && strings.TrimSpace(line) == "":
			inList = false
		case inList:
			name := strings.Fields(line)[0]
			if name != "help" && name != "completion" {
				subs = append(subs, name)
			}
		}
	}

	if len(subs) == 0 {
		return [][]string{prefix}
	}

	var all [][]string
	for _, s := range subs {
		all = append(all, leafCommands(append(append([]string(nil), prefix...), s))...)
	}

	return all
}
