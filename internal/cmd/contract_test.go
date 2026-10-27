package cmd

import (
	"strings"
	"testing"
)

// Отрицательные флаги — находка ревью без контекста 16.10; проверка в root.go.

func TestNegativeFlagRejected(t *testing.T) {
	f := newFakeCloud(t)
	for _, args := range [][]string{
		{"clusters", "resize", "c1", "--storage-gib", "-5"},
		{"clusters", "create", "--name", "n", "--version", "v", "--spec", "s", "--shards", "-1"},
	} {
		if _, err := run(t, args...); exitCode(err) != 1 || !strings.Contains(err.Error(), "non-negative") {
			t.Errorf("%v: %v", args, err)
		}
	}

	if _, err := runPaas(t, "", "kafka", "topics", "update", "k1", "t1", "--partitions", "-3"); exitCode(err) != 1 {
		t.Errorf("topics update: %v", err)
	}

	if len(f.calls) != 0 {
		t.Fatalf("запросы ушли: %v", f.calls)
	}
}

// Корень передаёт командам stdout, stderr и --json: пакетные тесты собирают
// State сами и этой связки не видят.
func TestRootWiresOutputStreamsAndJSON(t *testing.T) {
	f := newFakeCloud(t)
	f.on("GET /v1/versions", jsonReply(`{"versions":[{"name":"25.8"}]}`))
	f.on("POST /v1/clusters/c1/users/app:revokeRole", jsonReply(`{}`))

	out, msg, err := runStreams(t, "--json", "versions", "list")
	if err != nil || strings.TrimSpace(out) != `{"versions":[{"name":"25.8"}]}` || msg != "" {
		t.Errorf("--json: stdout %q, stderr %q, %v", out, msg, err)
	}

	out, msg, err = runStreams(t, "versions", "list")
	if err != nil || out != "25.8\n" || msg != "" {
		t.Errorf("таблица: stdout %q, stderr %q, %v", out, msg, err)
	}

	out, msg, err = runStreams(t, "users", "revoke", "c1", "app", "cloud_reader")
	if err != nil || out != "" || !strings.Contains(msg, "revoke: cloud_reader → app") {
		t.Errorf("сообщение: stdout %q, stderr %q, %v", out, msg, err)
	}
}
