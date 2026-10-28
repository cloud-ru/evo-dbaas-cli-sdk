package e2e_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

// scenario — один вызов бинарника против фейка.
type scenario struct {
	name      string             // имя golden; у commandRuns — args
	args      string             // аргументы через пробел; ENVFILE — путь во временной папке
	stdin     string             // что подать на вход
	env       []string           // дополнительные ИМЯ=значение
	unset     []string           // адреса фейка, которые не задавать: бинарник берёт умолчание
	proxy     bool               // HTTPS_PROXY на фейк: он записывает хост CONNECT
	noKey     bool               // без CLOUDRU_KEY_ID/CLOUDRU_KEY_SECRET
	overrides map[string]reply   // «МЕТОД /продукт/путь» → ответ
	seq       map[string][]reply // «МЕТОД /продукт/путь» → ответы по очереди, последний повторяется
	interrupt string             // когда в stderr появится эта строка — SIGINT, как Ctrl-C
}

// result — то, что фиксирует golden-файл.
type result struct {
	exit     int
	stdout   string
	stderr   string
	requests []string
}

func (r result) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "exit: %d\n--- stdout\n%s\n--- stderr\n%s\n--- requests\n", r.exit, r.stdout, r.stderr)

	for _, q := range r.requests {
		b.WriteString(q + "\n")
	}

	return b.String()
}

var (
	slugRe    = regexp.MustCompile(`[^a-z0-9а-яё]+`)
	versionRe = regexp.MustCompile(`(cloudru-dbaas version |cloudru-dbaas/)\S+`) // версия из VCS меняется с каждым коммитом
)

// goldenName — имя файла по аргументам: «redis clusters get c1» → redis_clusters_get_c1.
func goldenName(name string) string {
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if r := []rune(slug); len(r) > 80 {
		sum := sha256.Sum256([]byte(name))
		slug = string(r[:70]) + "_" + hex.EncodeToString(sum[:4])
	}

	return slug
}

func runScenario(s scenario) result {
	f := newFakeCloud(s.overrides, s.seq)
	DeferCleanup(f.close)

	tmp := GinkgoT().TempDir()
	args := strings.Fields(strings.ReplaceAll(s.args, "ENVFILE", filepath.Join(tmp, "key.env")))

	cmd := exec.Command(binPath, args...)
	cmd.Stdin = strings.NewReader(s.stdin)

	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + tmp,
		"XDG_CACHE_HOME=" + filepath.Join(tmp, "cache"),
		"CLOUDRU_PROJECT_ID=proj-1",
	}, f.env(s.unset)...)

	if s.proxy {
		cmd.Env = append(cmd.Env, "HTTPS_PROXY="+f.srv.URL)
	}

	if !s.noKey {
		cmd.Env = append(cmd.Env, "CLOUDRU_KEY_ID=kid", "CLOUDRU_KEY_SECRET=sec")
	}

	cmd.Env = append(cmd.Env, s.env...)

	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(session.Kill)

	if s.interrupt != "" {
		Eventually(session.Err).WithTimeout(gexecTimeout).Should(gbytes.Say(regexp.QuoteMeta(s.interrupt)))
		session.Interrupt()
	}

	Eventually(session).WithTimeout(gexecTimeout).Should(gexec.Exit())

	clean := func(s string) string {
		s = strings.ReplaceAll(s, f.srv.URL, "http://FAKE")
		s = strings.ReplaceAll(s, tmp, "TMP")
		s = versionRe.ReplaceAllString(s, "${1}VERSION")

		return strings.TrimRight(s, "\n")
	}

	reqs := f.requests()
	for i := range reqs {
		reqs[i] = clean(reqs[i])
	}

	return result{
		exit:     session.ExitCode(),
		stdout:   clean(string(session.Out.Contents())),
		stderr:   clean(string(session.Err.Contents())),
		requests: reqs,
	}
}

// matchGolden сравнивает с testdata/golden/<name>.golden; при E2E_UPDATE=1 перезаписывает.
func matchGolden(name string, got result) {
	path := filepath.Join("testdata", "golden", goldenName(name)+".golden")

	if os.Getenv("E2E_UPDATE") == "1" {
		if os.Getenv("CI") != "" {
			Fail("E2E_UPDATE=1 в CI: golden обновляют локально и проверяют дифф")
		}

		Expect(os.WriteFile(path, []byte(got.String()), 0o644)).To(Succeed())

		return
	}

	want, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred(), "нет golden %s — запусти с E2E_UPDATE=1 и проверь дифф", path)
	Expect(got.String()).To(Equal(string(want)), "сценарий %q разошёлся с %s", name, path)
}
