//go:build e2e

// Живой уровень сетки: тот же бинарник против настоящего cloud.ru.
// Создаёт платные кластеры минимальной спецификации и удаляет их. Без тега e2e файла нет в сборке,
// обычный go test его не видит. Запуск — продукты параллельно; создание Kafka занимает больше часа:
//
//	go run github.com/onsi/ginkgo/v2/ginkgo --procs 3 --tags e2e --label-filter live --timeout 5h ./tests/e2e
//
// Нужны CLOUDRU_KEY_ID, CLOUDRU_KEY_SECRET, CLOUDRU_PROJECT_ID, E2E_SUBNET_ID и E2E_ZONE_ID
// (зона этой подсети). Кластеры называются e2e-<продукт>-<время>; уборка — по id из create,
// без id — по имени.
package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

const (
	liveCall    = 2 * time.Minute   // чтение и запуск операции
	liveTimeout = "120m"            // --timeout для --wait: создание Kafka — больше часа
	liveWait    = 125 * time.Minute // liveTimeout и запас на выход
)

// liveProduct — что у продуктов различается: флаги версии и спецификаций, выбор спецификации, тело create.
type liveProduct struct {
	name    string
	version func(versions []map[string]any) string
	specs   func(version, zone string) []string
	fits    func(spec map[string]any) bool
	create  func(name, version, spec, storage, subnet string) []string
}

var liveProducts = []liveProduct{
	{
		name:    "redis",
		version: func(v []map[string]any) string { return str(v[0]["id"]) },
		specs: func(version, zone string) []string {
			return []string{"redis", "specs", "list", "--version-id", version, "--zone", zone}
		},
		fits: func(map[string]any) bool { return true },
		create: func(name, version, spec, storage, subnet string) []string {
			return []string{
				"redis", "clusters", "create", "--name", name, "--version-id", version, "--spec", spec,
				"--type", "standalone", "--storage-gib", storage, "--subnet-id", subnet,
			}
		},
	},
	{
		name:    "kafka",
		version: func(v []map[string]any) string { return str(v[0]["id"]) },
		specs: func(version, zone string) []string {
			return []string{"kafka", "specs", "list", "--version-id", version, "--zone", zone}
		},
		fits: func(s map[string]any) bool { return num(s["min_brokers"]) <= 1 },
		create: func(name, version, spec, storage, subnet string) []string {
			return []string{
				"kafka", "clusters", "create", "--name", name, "--version-id", version, "--spec", spec,
				"--brokers", "1", "--storage-gib", storage, "--subnet-id", subnet,
			}
		},
	},
	{
		name: "clickhouse",
		// Версии ClickHouse — только имена, последняя — самая новая.
		version: func(v []map[string]any) string { return str(v[len(v)-1]["name"]) },
		specs: func(version, zone string) []string {
			return []string{"clickhouse", "specs", "list", "--version", version, "--zone", zone, "--role", "server"}
		},
		fits: func(s map[string]any) bool { return num(s["min_replicas"]) <= 1 && num(s["min_shards"]) <= 1 },
		create: func(name, version, spec, storage, subnet string) []string {
			return []string{
				"clickhouse", "clusters", "create", "--name", name, "--version", version, "--spec", spec,
				"--shards", "1", "--replicas", "1", "--keeper", "embedded", "--storage-gib", storage,
				"--subnet-id", subnet,
			}
		},
	},
}

var _ = Describe("живой cloud.ru", Label("live"), func() {
	for _, p := range liveProducts {
		Describe(p.name, Ordered, func() {
			var id, name string

			BeforeAll(func() {
				subnet, zone := liveEnv("E2E_SUBNET_ID"), liveEnv("E2E_ZONE_ID")
				for _, k := range []string{"CLOUDRU_KEY_ID", "CLOUDRU_KEY_SECRET", "CLOUDRU_PROJECT_ID"} {
					liveEnv(k)
				}

				versions := liveList(liveCall, "versions", p.name, "versions", "list")
				Expect(versions).NotTo(BeEmpty(), "API не вернул версий")
				version := p.version(versions)

				spec, storage := smallestSpec(liveList(liveCall, "specifications", p.specs(version, zone)...), p.fits)
				Expect(storage).NotTo(Equal("0"), "у спецификации %s нет min_storage_gigs", spec)

				name = fmt.Sprintf("e2e-%s-%s", p.name, time.Now().UTC().Format("20060102-150405"))

				// Уборка регистрируется до create: обрыв посреди --wait не оставит платный кластер.
				// Внутри — ни одного Expect до delete: упавшая проверка оборвала бы уборку.
				// delete без --wait: удаление идёт в облаке, бюджет сьюта на него не тратится.
				DeferCleanup(func() {
					if id == "" {
						id = cleanupFind(p.name, name)
					}

					if id == "" {
						AddReportEntry("уборка: кластер не найден", name)
						return
					}

					if s := run(liveCall, p.name, "clusters", "delete", id, "--yes"); s == nil || s.ExitCode() != 0 {
						AddReportEntry("уборка: кластер не удалён — удалить руками", p.name+" "+id)
						Fail(fmt.Sprintf("уборка: %s %s не удалён", p.name, id))
					}
				})

				args := append(p.create(name, version, spec, storage, subnet), "--wait", "--timeout", liveTimeout)
				s := live(liveWait, args...)

				// id — до проверки кода: при откате или коде 8 кластер уже есть, уборке нужен id.
				id = createdID(s.Out.Contents())
				Expect(s.ExitCode()).To(Equal(0), "create --wait: %s", s.Err.Contents())

				if id == "" {
					id = findByName(p.name, name)
				}

				Expect(id).NotTo(BeEmpty(), "create не вернул id, по имени %s не найден", name)
			})

			It("get показывает созданный кластер", func() {
				c := liveCluster(live(liveCall, p.name, "clusters", "get", id))
				Expect(c["id"]).To(Equal(id))
				Expect(c["name"]).To(Equal(name))
			})

			It("list находит кластер", func() {
				Expect(findByName(p.name, name)).To(Equal(id))
			})

			It("delete --wait удаляет: get отдаёт ERASED, list не находит", func() {
				s := live(liveWait, p.name, "clusters", "delete", id, "--yes", "--wait", "--timeout", liveTimeout)
				Expect(s.ExitCode()).To(Equal(0), "delete --wait: %s", s.Err.Contents())

				deleted := id
				id = "" // уборке больше нечего удалять

				// Удалённый кластер get отдаёт и дальше, со статусом ERASED (все три продукта).
				Eventually(func() any {
					return liveCluster(live(liveCall, p.name, "clusters", "get", deleted))["status"]
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Equal("ERASED"))
				Expect(findByName(p.name, name)).To(BeEmpty())
			})
		})
	}
})

// live — run с проверкой, что бинарник запустился и вышел до limit.
func live(limit time.Duration, args ...string) *gexec.Session {
	s := run(limit, args...)
	Expect(s).NotTo(BeNil(), "%v: не запустился или не вышел за %s", args, limit)

	return s
}

// run запускает бинарник с --json и ждёт выхода не дольше limit; не вышел — убивает, nil.
// Без Expect: его зовёт уборка. Временная папка — своя, не GinkgoT().TempDir(): тот
// регистрирует DeferCleanup, а в уборке Ginkgo это запрещает (уборка не удалила бы кластеры).
func run(limit time.Duration, args ...string) *gexec.Session {
	tmp, err := os.MkdirTemp("", "e2e-live-")
	if err != nil {
		return nil
	}

	defer func() { _ = os.RemoveAll(tmp) }()

	args = append(args, "--json")
	cmd := exec.Command(binPath, args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + tmp,
		"XDG_CACHE_HOME=" + filepath.Join(tmp, "cache"),
		"CLOUDRU_KEY_ID=" + os.Getenv("CLOUDRU_KEY_ID"),
		"CLOUDRU_KEY_SECRET=" + os.Getenv("CLOUDRU_KEY_SECRET"),
		"CLOUDRU_PROJECT_ID=" + os.Getenv("CLOUDRU_PROJECT_ID"),
	}

	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	if err != nil {
		return nil
	}

	select {
	case <-session.Exited:
		return session
	case <-time.After(limit):
		session.Kill().Wait() // до удаления HOME из-под живого процесса

		return nil
	}
}

func liveEnv(key string) string {
	v := os.Getenv(key)
	Expect(v).NotTo(BeEmpty(), "живой уровень: не задана %s", key)

	return v
}

// liveList — массив по ключу из ответа --json.
func liveList(limit time.Duration, key string, args ...string) []map[string]any {
	s := live(limit, args...)
	Expect(s.ExitCode()).To(Equal(0), "%v: %s", args, s.Err.Contents())

	var body map[string][]map[string]any
	Expect(json.Unmarshal(s.Out.Contents(), &body)).To(Succeed())

	return body[key]
}

// smallestSpec — подходящая спецификация с наименьшими CPU и памятью и её минимальный диск.
func smallestSpec(specs []map[string]any, fits func(map[string]any) bool) (string, string) {
	var ok []map[string]any

	for _, s := range specs {
		if fits(s) {
			ok = append(ok, s)
		}
	}

	Expect(ok).NotTo(BeEmpty(), "нет подходящей спецификации")
	sort.SliceStable(ok, func(i, j int) bool {
		if num(ok[i]["cpu"]) != num(ok[j]["cpu"]) {
			return num(ok[i]["cpu"]) < num(ok[j]["cpu"])
		}

		return num(ok[i]["memory"]) < num(ok[j]["memory"])
	})

	return str(ok[0]["id"]), fmt.Sprint(num(ok[0]["min_storage_gigs"]))
}

// createdID — id из вывода create --wait: кластер или, без resource_id, завершённая операция.
func createdID(out []byte) string {
	var body map[string]any
	if json.Unmarshal(out, &body) != nil {
		return ""
	}

	if _, op := body["done"]; op {
		return str(body["resource_id"])
	}

	if c, ok := body["cluster"].(map[string]any); ok {
		return str(c["id"])
	}

	return str(body["id"])
}

// liveCluster — кластер из ответа get, плоского или под ключом cluster.
func liveCluster(s *gexec.Session) map[string]any {
	Expect(s.ExitCode()).To(Equal(0), "get: %s", s.Err.Contents())

	var body map[string]any
	Expect(json.Unmarshal(s.Out.Contents(), &body)).To(Succeed())

	if c, ok := body["cluster"].(map[string]any); ok {
		return c
	}

	return body
}

// cleanupFind — findByName для уборки: без Expect, три попытки на разовый сбой list.
func cleanupFind(product, name string) string {
	for range 3 {
		if s := run(liveCall, product, "clusters", "list"); s != nil && s.ExitCode() == 0 {
			var body map[string][]map[string]any
			if json.Unmarshal(s.Out.Contents(), &body) == nil {
				for _, c := range body["clusters"] {
					if c["name"] == name {
						return str(c["id"])
					}
				}

				return ""
			}
		}

		time.Sleep(10 * time.Second)
	}

	return ""
}

func findByName(product, name string) string {
	for _, c := range liveList(liveCall, "clusters", product, "clusters", "list") {
		if c["name"] == name {
			return str(c["id"])
		}
	}

	return ""
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) int {
	f, _ := v.(float64)
	return int(f)
}
