package iam

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// env-файл подключают через «. FILE», значение не должно выполниться.
func TestShellValueSurvivesSource(t *testing.T) {
	for _, v := range []string{"KID9", "a/b+c=", "x'y", "$(touch PWNED)", "a b", "line1\nline2", "`id`", ""} {
		dir := t.TempDir()

		f := filepath.Join(dir, "k.env")
		if err := os.WriteFile(f, []byte("V="+shellValue(v)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command("sh", "-c", `set -a; . ./k.env; set +a; printf %s "$V"`)
		cmd.Dir = dir

		out, err := cmd.Output()
		if err != nil || string(out) != v {
			t.Errorf("%q: source вернул %q, err=%v", v, out, err)
		}

		if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
			t.Errorf("%q: source выполнил команду", v)
		}
	}

	if got := shellValue("KID9"); got != "KID9" {
		t.Errorf("безопасное значение без кавычек, получили %q", got)
	}
}
