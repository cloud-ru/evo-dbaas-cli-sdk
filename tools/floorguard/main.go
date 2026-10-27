// Command floorguard проверяет «пол» из CONSTRAINTS.md на дифе от merge-base и ловит ходы,
// которые опускают планку. Ужесточение проходит молча, ослабление — громко.
//
//   - Код и тесты (нетто по дифу, переносы не считаются): новые заглушки линтера, пропуски
//     и фокус тестов, заглушки вместо кода, build-теги, выключающие файлы; проверок стало меньше.
//
//   - Охраняемые файлы — по смыслу, а не по строкам: .golangci.yaml и Taskfile.yml разбираются
//     как YAML (новое должно быть не слабее базы), CONSTRAINTS.md — по пунктам и строкам таблиц.
//
//   - Сам страж, CI и git-хуки (tools/, .github/workflows/, .githooks/) — любая правка
//     существующего файла — решение человека.
//
//     go run ./tools/floorguard [-base origin/main]
//
// Код выхода: 0 — чисто, 1 — нарушение пола, 2 — не смог проверить.
// Идея и пять ходов — эталон floor-guard из скилла constraint-driven-development (addyosmani/agent-skills).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

type line struct{ file, text string }

type finding struct{ rule, file, text string }

func main() {
	base := flag.String("base", "origin/main", "с чем сравнивать (merge-base с HEAD)")

	flag.Parse()

	findings, err := check(*base)
	if errors.Is(err, errRootCommit) {
		fmt.Println("floorguard: первый коммит репо, сравнивать не с чем")

		return
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "floorguard:", err)
		os.Exit(2)
	}

	if len(findings) == 0 {
		fmt.Println("floorguard: чисто")

		return
	}

	fmt.Fprintf(os.Stderr, "floorguard: нарушений пола — %d:\n", len(findings))

	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "  [%s] %s: %s\n", f.rule, f.file, f.text)
	}

	fmt.Fprintln(os.Stderr, "\nКаждое опускает планку. Почините код; если ослабление нужно — это решение мейнтейнера, отдельным PR.")
	os.Exit(1)
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return string(out), nil
}

// errRootCommit — у HEAD нет родителя, а база не находится: так выглядит снимок релиза
// одним коммитом, где CI сравнивает с HEAD^.
var errRootCommit = errors.New("первый коммит репо")

func check(base string) ([]finding, error) {
	mb, err := git("merge-base", base, "HEAD")
	if err != nil {
		if parents, perr := git("rev-list", "--parents", "-n", "1", "HEAD"); perr == nil &&
			len(strings.Fields(parents)) == 1 {
			return nil, errRootCommit
		}

		return nil, err
	}

	mb = strings.TrimSpace(mb)

	diff, err := git("diff", "--unified=0", "--no-renames", mb, "--")
	if err != nil {
		return nil, err
	}

	names, err := git("diff", "--name-only", "--no-renames", mb, "--")
	if err != nil {
		return nil, err
	}

	untracked, err := git("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}

	for f := range strings.FieldsSeq(untracked) {
		// --no-index выходит с кодом 1, когда разница есть, — это норма.
		out, _ := exec.Command("git", "diff", "--no-index", "--unified=0", "/dev/null", f).Output()
		diff += "\n" + string(out)
	}

	added, removed := parse(diff)
	findings := codeFindings(added, removed)

	// Удалённый golden — удалённый сценарий сетки e2e.
	status, err := git("diff", "--name-status", "--no-renames", mb, "--")
	if err != nil {
		return nil, err
	}

	for l := range strings.SplitSeq(status, "\n") {
		if f, ok := strings.CutPrefix(l, "D\t"); ok && strings.HasPrefix(f, "tests/e2e/testdata/golden/") {
			findings = append(findings, finding{"сценарий-e2e-удалён", f, "удаление сценария сетки — " + human})
		}
	}

	// Стража в базе нет — он только появляется, охрану не применяем.
	if _, err := git("cat-file", "-e", mb+":"+bootstrapFile); err != nil {
		return findings, nil
	}

	for _, path := range strings.Fields(names + "\n" + untracked) {
		if !isGuarded(path) {
			continue
		}

		var old, cur []byte

		if s, err := git("show", mb+":"+path); err == nil {
			old = []byte(s)
		}

		cur, err = os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		gf, err := guarded(path, old, cur)
		if err != nil {
			return nil, err
		}

		findings = append(findings, gf...)
	}

	return findings, nil
}

func parse(diff string) (added, removed []line) {
	var oldFile, newFile string

	for l := range strings.SplitSeq(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "--- "):
			oldFile = strings.TrimPrefix(strings.TrimPrefix(l, "--- "), "a/")
		case strings.HasPrefix(l, "+++ "):
			newFile = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
		case strings.HasPrefix(l, "+"):
			added = append(added, line{newFile, l[1:]})
		case strings.HasPrefix(l, "-"):
			removed = append(removed, line{oldFile, l[1:]})
		}
	}

	return added, removed
}
