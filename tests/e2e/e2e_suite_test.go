// Package e2e_test — сетка e2e: настоящий бинарник против
// фейкового облака на httptest. Сценарий фиксирует код выхода, stdout, stderr и запросы к API
// в golden-файле testdata/golden/<имя>.golden; рефакторинг их не меняет, а PR, меняющий
// поведение, обновляет golden осознанно: E2E_UPDATE=1 go test ./tests/e2e/ (в CI — запрещено).
// Повтор прогона — ginkgo --repeat N: go test -count>1 Ginkgo не поддерживает.
package e2e_test

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gexec"
)

var binPath string

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cloudru-dbaas e2e")
}

// Бинарник собирает первый процесс, остальные (ginkgo -p) получают путь.
var _ = SynchronizedBeforeSuite(func() []byte {
	readSources()

	path, err := gexec.Build("github.com/cloud-ru/evo-dbaas-cli-sdk/cmd/cloudru-dbaas")
	Expect(err).NotTo(HaveOccurred())

	return []byte(path)
}, func(path []byte) {
	binPath = string(path)
	format.MaxLength = 0 // дифф golden печатать целиком
})

var _ = SynchronizedAfterSuite(func() {}, gexec.CleanupBuildArtifacts)

// readSources читает исходники бинарника. gexec собирает его в подпроцессе, и go test не видит
// этих зависимостей: после правки CLI результат пришёл бы из кэша. Файлы, открытые самим
// тестом, go test кладёт в ключ кэша.
func readSources() {
	module := os.DirFS("../..")

	for _, root := range []string{"cmd", "internal", "go.mod", "go.sum"} {
		err := fs.WalkDir(module, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}

			if strings.HasSuffix(path, ".go") || path == "go.mod" || path == "go.sum" {
				if _, err := fs.ReadFile(module, path); err != nil {
					return fmt.Errorf("исходник бинарника: %w", err)
				}
			}

			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	}
}
