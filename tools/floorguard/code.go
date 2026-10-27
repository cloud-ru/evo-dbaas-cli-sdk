package main

import (
	"fmt"
	"regexp"
	"strings"
)

// pattern — ход «дешёвой дороги к зелёному» в коде. Считается нетто по всему дифу:
// перенос строки между файлами (рефакторинг) не нарушение, рост — нарушение.
type pattern struct {
	rule   string
	re     *regexp.Regexp
	scope  func(file string) bool
	except *regexp.Regexp // совпадение — не нарушение
}

var patterns = []pattern{
	{"линтер-заглушён", regexp.MustCompile(`//\s*nolint|#nosec|//\s*lint:ignore`), isGo, nil},
	{"тест-пропущен-или-сфокусирован", regexp.MustCompile(
		`\bSkip(f|Now)?\(|\b(PIt|XIt|PDescribe|XDescribe|PContext|XContext|PEntry|XEntry|PDescribeTable|PWhen)\(|` +
			`\bPending\b|\bFocus\b|\bF(It|Describe|Context|Entry|DescribeTable|When)\(`), isTest, nil},
	{"недоделано", regexp.MustCompile(`panic\("(not implemented|TODO|unimplemented)|\bTODO\b|\bFIXME\b`), isGo, nil},
	// Build-тег выключает файл из сборки и тестов; любой, включая e2e (живой уровень сетки), — решение человека.
	{"build-тег", regexp.MustCompile(`^\s*//\s*(go:build|\+build)\b`), isGo, nil},
	// Заголовок генератора выводит файл из-под линтера и храповика — только у файлов генераторов:
	// *.gen.go (oapi-codegen) и mocks_test.go (mockery).
	{"ложный-заголовок-генератора", regexp.MustCompile(`^// Code generated .*DO NOT EDIT`), notGenerated, nil},
}

// Проверки в тестах.
var assertions = regexp.MustCompile(`\b(assert|require)\.[A-Z]|\bExpect\(|\bEventually\(|\bConsistently\(|\bt\.(Error|Fatal)f?\(`)

// Сам страж охраняется целиком (isGuarded), а его тесты содержат образцы ходов — не сканируем.
func ownFile(f string) bool { return strings.HasPrefix(f, "tools/floorguard/") }

func (p pattern) hit(l line) bool {
	return p.scope(l.file) && p.re.MatchString(l.text) && (p.except == nil || !p.except.MatchString(l.text))
}

// По имени файла, не по каталогу: в каталоге клиента API лежит и ручной код (internal/gen/clickhouse/flexint.go).
var generatedPlace = regexp.MustCompile(`\.gen\.go$|(^|/)mocks_test\.go$`)

func notGenerated(f string) bool { return isGo(f) && !generatedPlace.MatchString(f) }

func isGo(f string) bool { return strings.HasSuffix(f, ".go") && !ownFile(f) }

func isTest(f string) bool {
	return (strings.HasSuffix(f, "_test.go") || strings.HasPrefix(f, "tests/")) && !ownFile(f)
}

func codeFindings(added, removed []line) []finding {
	var out []finding

	for _, p := range patterns {
		var plus []line

		minus := map[string]int{}

		for _, l := range removed {
			if p.hit(l) {
				minus[strings.TrimSpace(l.text)]++
			}
		}

		for _, l := range added {
			if p.hit(l) {
				plus = append(plus, l)
			}
		}

		// Добавленная строка, равная удалённой, — перенос.
		for _, l := range plus {
			t := strings.TrimSpace(l.text)
			if minus[t] > 0 {
				minus[t]--

				continue
			}

			out = append(out, finding{p.rule, l.file, short(t)})
		}
	}

	plus, minus := 0, 0

	for _, l := range added {
		if isTest(l.file) && assertions.MatchString(l.text) {
			plus++
		}
	}

	for _, l := range removed {
		if isTest(l.file) && assertions.MatchString(l.text) {
			minus++
		}
	}

	if minus > plus {
		out = append(out, finding{
			"проверок-в-тестах-стало-меньше", "*_test.go, tests/",
			fmt.Sprintf("убрано %d, добавлено %d", minus, plus),
		})
	}

	return out
}

func short(s string) string {
	if r := []rune(s); len(r) > 120 {
		return string(r[:120])
	}

	return s
}
