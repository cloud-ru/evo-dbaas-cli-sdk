// Command ratchet сверяет измеримые числа с таблицей «Измеряем» в CONSTRAINTS.md:
// ручные строки Go не растут, покрытие ручного кода не падает больше допуска.
//
//	go run ./tools/ratchet -cover cover.out -tests test.json
//
// Пройденные тесты считаются по выводу go test -json: любое выключение теста — переименование
// под чужую ОС, перенос в testdata/, build-тег, Skip — уменьшает число.
//
// Код выхода: 0 — в пределах, 1 — хуже записанного, 2 — не смог посчитать.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func main() {
	coverFile := flag.String("cover", "", "профиль go test -coverprofile; без него покрытие не сверяется")
	testsFile := flag.String("tests", "", "вывод go test -json; без него тесты не сверяются")
	failures := flag.String("failures", "", "только напечатать упавшие тесты из вывода go test -json")

	flag.Parse()

	if *failures != "" {
		if err := printFailures(*failures, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "ratchet:", err)
		}

		return
	}

	code, err := run(*coverFile, *testsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ratchet:", err)
	}

	os.Exit(code)
}

func run(coverFile, testsFile string) (int, error) {
	recorded, err := readRecorded("CONSTRAINTS.md")
	if err != nil {
		return 2, err
	}

	lines, err := manualLines()
	if err != nil {
		return 2, err
	}

	code := 0

	fmt.Printf("ручные строки Go: %d (записано %d)\n", lines, int(recorded["lines"]))

	if float64(lines) > recorded["lines"] {
		fmt.Fprintln(os.Stderr, "ratchet: ручных строк больше записанного — уберите код или объясните рост в PR и поднимите число отдельным коммитом")

		code = 1
	}

	if testsFile != "" {
		passed, err := passedTests(testsFile)
		if err != nil {
			return 2, err
		}

		fmt.Printf("пройденные тесты: %d (записано %d)\n", passed, int(recorded["tests"]))

		if float64(passed) < recorded["tests"] {
			fmt.Fprintln(os.Stderr, "ratchet: пройденных тестов меньше записанного — тест выключен, пропущен или удалён")

			code = 1
		}
	}

	if coverFile == "" {
		return code, nil
	}

	generated, err := generatedFiles()
	if err != nil {
		return 2, err
	}

	cover, err := manualCoverage(coverFile, generated)
	if err != nil {
		return 2, err
	}

	fmt.Printf("покрытие ручного кода: %.1f%% (записано %.1f%%, допуск %.1f)\n", cover, recorded["cover"], recorded["tolerance"])

	if cover < recorded["cover"]-recorded["tolerance"] {
		fmt.Fprintln(os.Stderr, "ratchet: покрытие упало ниже записанного — добавьте тесты")

		code = 1
	}

	return code, nil
}

var (
	rowRe       = regexp.MustCompile(`^\|\s*(Ручные строки Go|Покрытие ручного кода|Пройденные тесты)\s*\|\s*([\d.,]+)`)
	toleranceRe = regexp.MustCompile(`допуск\s+([\d.,]+)`)
	generatedRe = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)
)

func parseNum(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil {
		return 0, fmt.Errorf("число %q: %w", s, err)
	}

	return v, nil
}

// readRecorded берёт числа из строк таблицы «Измеряем» в CONSTRAINTS.md.
func readRecorded(path string) (map[string]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	got := map[string]float64{}
	sc := bufio.NewScanner(bytes.NewReader(data))

	for sc.Scan() {
		m := rowRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}

		v, err := parseNum(m[2])
		if err != nil {
			return nil, err
		}

		switch m[1] {
		case "Ручные строки Go":
			got["lines"] = v

			continue
		case "Пройденные тесты":
			got["tests"] = v

			continue
		}

		got["cover"] = v

		t := toleranceRe.FindStringSubmatch(sc.Text())
		if t == nil {
			return nil, errors.New("в строке «Покрытие ручного кода» нет «допуск N»")
		}

		if got["tolerance"], err = parseNum(t[1]); err != nil {
			return nil, err
		}
	}

	if _, ok := got["lines"]; !ok {
		return nil, errors.New("в CONSTRAINTS.md нет строки «Ручные строки Go»")
	}

	if _, ok := got["tests"]; !ok {
		return nil, errors.New("в CONSTRAINTS.md нет строки «Пройденные тесты»")
	}

	if _, ok := got["cover"]; !ok {
		return nil, errors.New("в CONSTRAINTS.md нет строки «Покрытие ручного кода»")
	}

	return got, nil
}

// isManual — ручной код: Go, не тест, не служебные tools/. Сгенерированный отсекается по
// заголовку «Code generated … DO NOT EDIT.», а не по имени файла.
func isManual(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") && !strings.HasPrefix(path, "tools/")
}

// testEvent — событие go test -json (cmd/test2json).
type testEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

// passedTests — сколько тестов и подтестов прошло (Action pass с именем теста).
func passedTests(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}

	n := 0

	for line := range strings.SplitSeq(string(data), "\n") {
		var ev testEvent
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Action == "pass" && ev.Test != "" {
			n++
		}
	}

	return n, nil
}

// printFailures печатает вывод упавших тестов, пакетов и сборки из go test -json.
func printFailures(path string, w io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	type key struct{ pkg, test string }

	outputs := map[key][]string{}

	var failed []key

	for line := range strings.SplitSeq(string(data), "\n") {
		var ev testEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			if line != "" {
				fmt.Fprintln(w, line) // не JSON — сообщение go test (сборка)
			}

			continue
		}

		k := key{ev.Package, ev.Test}

		switch ev.Action {
		case "output", "build-output":
			outputs[k] = append(outputs[k], ev.Output)
		case "fail", "build-fail":
			failed = append(failed, k)
		}
	}

	for _, k := range failed {
		fmt.Fprint(w, strings.Join(outputs[k], ""))
	}

	return nil
}

// generatedFiles — сгенерированные файлы в виде путей профиля покрытия (модуль/путь).
func generatedFiles() (map[string]bool, error) {
	mod, err := exec.Command("go", "list", "-m").Output()
	if err != nil {
		return nil, fmt.Errorf("go list -m: %w", err)
	}

	out, err := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "*.go").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	gen := map[string]bool{}

	for _, path := range strings.Fields(string(out)) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		if generatedRe.Match(data) {
			gen[strings.TrimSpace(string(mod))+"/"+path] = true
		}
	}

	return gen, nil
}

// manualLines — непустые строки ручного кода без строк-комментариев.
func manualLines() (int, error) {
	out, err := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "*.go").Output()
	if err != nil {
		return 0, fmt.Errorf("git ls-files: %w", err)
	}

	total := 0

	for _, path := range strings.Fields(string(out)) {
		if !isManual(path) {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", path, err)
		}

		if generatedRe.Match(data) {
			continue
		}

		for line := range strings.SplitSeq(string(data), "\n") {
			t := strings.TrimSpace(line)
			if t != "" && !strings.HasPrefix(t, "//") {
				total++
			}
		}
	}

	return total, nil
}

// manualCoverage — доля покрытых операторов ручного кода по профилю go test.
func manualCoverage(path string, generated map[string]bool) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}

	var all, covered int

	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}

		// github.com/x/y/file.go:1.2,3.4 <операторов> <счётчик>
		name, rest, ok := strings.Cut(line, ":")
		f := strings.Fields(rest)

		if !ok || len(f) != 3 || generated[name] || strings.Contains(name, "/tools/") {
			continue
		}

		n, err1 := strconv.Atoi(f[1])
		c, err2 := strconv.Atoi(f[2])

		if err1 != nil || err2 != nil {
			return 0, fmt.Errorf("строка профиля %q", line)
		}

		all += n
		if c > 0 {
			covered += n
		}
	}

	if all == 0 {
		return 0, errors.New("в профиле нет ручного кода")
	}

	return 100 * float64(covered) / float64(all), nil
}
