package main

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	exceptionRow = regexp.MustCompile(`^\|\s*W\d+\s*\|`)
	ratchetRow   = regexp.MustCompile(`^\|\s*(Ручные строки Go|Покрытие ручного кода|Пройденные тесты)\s*\|`)
	numberRe     = regexp.MustCompile(`\d+(?:[.,]\d+)?`)
)

// compareConstraints: CONSTRAINTS.md меняет человек. Агенту разрешено удалить строку
// исключения (W…) и сдвинуть числа в строке храповика в строгую сторону при том же тексте.
func compareConstraints(oldS, curS string) []finding {
	if oldS == "" || curS == "" {
		return []finding{{"constraints-изменён", "CONSTRAINTS.md", "появление или удаление файла — " + human}}
	}

	cur := strings.Split(curS, "\n")

	var old []string

	for _, l := range strings.Split(oldS, "\n") {
		// Строка исключения, которой больше нет, — снята.
		if exceptionRow.MatchString(l) && !contains(cur, l) {
			continue
		}

		old = append(old, l)
	}

	if len(old) != len(cur) {
		return []finding{{"constraints-изменён", "CONSTRAINTS.md", "добавлены или удалены строки — " + human}}
	}

	var out []finding

	for i := range old {
		if old[i] == cur[i] {
			continue
		}

		rule := "constraints-изменён"
		if ratchetRow.MatchString(old[i]) {
			if msg := compareRatchet(old[i], cur[i]); msg == "" {
				continue
			} else {
				rule = msg
			}
		}

		out = append(out, finding{rule, "CONSTRAINTS.md", short(old[i] + "  →  " + cur[i])})
	}

	return out
}

// compareRatchet — тот же текст, числа сдвинуты в строгую сторону: для «Ручные строки Go»
// все числа не растут; для прочих первое (порог) не падает, остальные (допуск) не растут.
func compareRatchet(o, c string) string {
	if numberRe.ReplaceAllString(o, "#") != numberRe.ReplaceAllString(c, "#") {
		return "constraints-изменён"
	}

	on, cn := numbers(o), numbers(c)
	lowerAll := strings.Contains(o, "Ручные строки")

	for i := range on {
		lower := lowerAll || i > 0
		if (lower && cn[i] > on[i]) || (!lower && cn[i] < on[i]) {
			return "порог-ослаблен"
		}
	}

	return ""
}

func numbers(s string) []float64 {
	var out []float64

	for _, m := range numberRe.FindAllString(s, -1) {
		v, _ := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64) // numberRe гарантирует формат
		out = append(out, v)
	}

	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}

	return false
}
