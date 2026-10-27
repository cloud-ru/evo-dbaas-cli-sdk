package main

import (
	"fmt"
	"reflect"
	"strings"

	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

// Охраняемое по умолчанию меняет только человек. Агенту разрешены ровно два хода, оба
// ужесточают планку: снять исключение линтера (правило в .golangci.yaml и строку W… в
// CONSTRAINTS.md) и сдвинуть число храповика в CONSTRAINTS.md в строгую сторону.
// Перечислять лазейки бесполезно (три раунда ревью это показали) — разрешённое перечислено.

// humanOnly — сам страж, сборка гейтов, CI, хуки, генератор (по нему gencheck судит, что сгенерировано),
// .gitignore и подмена зависимостей мимо go.mod (go.work, vendor/): любое изменение, включая новый файл.
var humanOnly = []string{
	"tools/", ".github/", ".githooks/", "Taskfile.yml", "scripts/generate.sh", ".gitignore", "go.work", "vendor/",
}

// bootstrapFile — пока его нет в базе, страж только появляется и охрану не применяет.
const bootstrapFile = "tools/floorguard/main.go"

func isGuarded(path string) bool {
	if path == ".golangci.yaml" || path == "CONSTRAINTS.md" || path == "go.mod" || isGeneratorConfig(path) {
		return true
	}

	for _, p := range humanOnly {
		if strings.HasPrefix(path, p) {
			return true
		}
	}

	return false
}

// Конфиги генераторов решают, какой код выйдет из-под линтера и храповика (заголовок «Code generated»):
// через user-templates oapi-codegen и template mockery в сгенерированный файл попадает любой код, и генератор
// воспроизводит его байт в байт. Поэтому что генерировать и куда — решение человека, как и сам генератор.
func isGeneratorConfig(path string) bool {
	return path == ".mockery.yml" || strings.HasPrefix(path, "internal/") && strings.HasSuffix(path, "/cfg.yaml")
}

const human = "решение человека"

// guarded сравнивает базовую (old, nil — не было) и текущую (cur, nil — удалён) версии.
func guarded(path string, old, cur []byte) ([]finding, error) {
	if string(old) == string(cur) && (old == nil) == (cur == nil) {
		return nil, nil
	}

	one := func(rule, text string) []finding { return []finding{{rule, path, text}} }

	switch path {
	case ".golangci.yaml":
		ok, err := lintOnlyDroppedRules(old, cur)
		if err != nil || ok {
			return nil, err
		}

		return one("конфигурация-линтера-изменена", "разрешено только снять правило-исключение — остальное "+human), nil
	case "CONSTRAINTS.md":
		return compareConstraints(string(old), string(cur)), nil
	case "go.mod":
		ok, err := modOnlyDroppedReplaces(old, cur)
		if err != nil || ok {
			return nil, err
		}

		return one("go.mod-replace", "replace подменяет зависимость — "+human), nil
	}

	return one("правка-стража-или-гейтов", "tools/, Taskfile.yml, CI, хуки, генератор, .gitignore, go.work, vendor/ — "+human), nil
}

// modOnlyDroppedReplaces проверяет, что каждая текущая подмена уже была в базе
// с теми же модулями и версиями по обе стороны =>.
func modOnlyDroppedReplaces(oldB, curB []byte) (bool, error) {
	old, err := modfile.Parse("go.mod", oldB, nil)
	if err != nil {
		return false, fmt.Errorf("go.mod в базе: %w", err)
	}

	cur, err := modfile.Parse("go.mod", curB, nil)
	if err != nil {
		return false, fmt.Errorf("go.mod: %w", err)
	}

	replaces := make(map[[4]string]bool, len(old.Replace))
	for _, r := range old.Replace {
		replaces[[4]string{r.Old.Path, r.Old.Version, r.New.Path, r.New.Version}] = true
	}

	for _, r := range cur.Replace {
		if !replaces[[4]string{r.Old.Path, r.Old.Version, r.New.Path, r.New.Version}] {
			return false, nil
		}
	}

	return true, nil
}

// lintOnlyDroppedRules — новая конфигурация равна старой без части правил exclusions.rules.
func lintOnlyDroppedRules(oldB, curB []byte) (bool, error) {
	if oldB == nil || curB == nil {
		return false, nil
	}

	var old, cur map[string]any

	if err := yaml.Unmarshal(oldB, &old); err != nil {
		return false, fmt.Errorf(".golangci.yaml в базе: %w", err)
	}

	if err := yaml.Unmarshal(curB, &cur); err != nil {
		return false, fmt.Errorf(".golangci.yaml: %w", err)
	}

	oldRules := exclusionRules(old)
	curRules := exclusionRules(cur)

	// Оставляем в старой конфигурации только те правила, что есть в новой, в том же порядке.
	var kept []any

	i := 0

	for _, r := range oldRules {
		if i < len(curRules) && reflect.DeepEqual(r, curRules[i]) {
			kept = append(kept, r)
			i++
		}
	}

	if i != len(curRules) {
		return false, nil // в новой есть правило, которого не было, или порядок другой
	}

	setExclusionRules(old, kept)
	setExclusionRules(cur, kept)

	return reflect.DeepEqual(old, cur), nil
}

func exclusions(cfg map[string]any) map[string]any {
	l, _ := cfg["linters"].(map[string]any)
	e, _ := l["exclusions"].(map[string]any)

	return e
}

func exclusionRules(cfg map[string]any) []any {
	r, _ := exclusions(cfg)["rules"].([]any)

	return r
}

func setExclusionRules(cfg map[string]any, rules []any) {
	if e := exclusions(cfg); e != nil {
		e["rules"] = rules
	}
}
