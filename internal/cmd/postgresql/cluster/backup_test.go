package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

// Сетка отправляет оба поля. Эта матрица ловит сброс второго поля, потерю
// явно заданного нуля/пустой строки и отправку backup без соответствующих флагов.
func TestClusterBackupFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		flags         []string
		current, want string
	}{
		{"schedule keeps days", []string{"--backup-schedule", "15 14 ? * *"}, `{"schedule":"0 3 * * 0","retention_policy_days":14}`, `{"schedule":"15 14 ? * *","retention_policy_days":14}`},
		{"schedule keeps days without current schedule", []string{"--backup-schedule", "0 6 * * *"}, `{"retention_policy_days":14}`, `{"schedule":"0 6 * * *","retention_policy_days":14}`},
		{"days keep schedule", []string{"--backup-retention-days", "30"}, `{"schedule":"0 3 * * 0","retention_policy_days":14}`, `{"schedule":"0 3 * * 0","retention_policy_days":30}`},
		{"both", []string{"--backup-schedule", "0 6 * * *", "--backup-retention-days", "7"}, `{"schedule":"0 3 * * 0","retention_policy_days":14}`, `{"schedule":"0 6 * * *","retention_policy_days":7}`},
		{"explicit zero", []string{"--backup-retention-days", "0"}, `{"schedule":"0 3 * * 0","retention_policy_days":14}`, `{"schedule":"0 3 * * 0","retention_policy_days":0}`},
		{"explicit empty", []string{"--backup-schedule", ""}, `{"schedule":"0 3 * * 0","retention_policy_days":14}`, `{"schedule":"","retention_policy_days":14}`},
		{"absent current backup", []string{"--backup-schedule", "0 3 * * 0"}, `null`, `{"schedule":"0 3 * * 0"}`},
		{"absent current schedule", []string{"--backup-retention-days", "7"}, `{"retention_policy_days":14}`, `{"retention_policy_days":7}`},
		{"absent current days", []string{"--backup-schedule", "0 3 * * 0"}, `{"schedule":"old"}`, `{"schedule":"0 3 * * 0"}`},
		{"unrelated update", []string{"--instances", "1"}, `{"schedule":"0 3 * * 0","retention_policy_days":14}`, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{
				"GET /v1/clusters/c1":    `{"version":"17","specification_id":"s1","instances":1,"subnet_ids":["sn1"],"backup":` + tc.current + `}`,
				"GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"BUSINESS"}]}`,
				"PATCH /v1/clusters/c1":  `{"id":"op1"}`,
			})
			out, err := run(t, f, true, append([]string{"update", "c1"}, tc.flags...)...)
			require.NoError(t, err)
			assert.Contains(t, out, "op1")

			var body struct {
				Backup map[string]any `json:"backup"`
			}
			require.NoError(t, output.Decode([]byte(f.body("PATCH /v1/clusters/c1")), &body))

			if tc.want == "" {
				assert.Nil(t, body.Backup)
			} else {
				var want map[string]any
				require.NoError(t, output.Decode([]byte(tc.want), &want))
				assert.Equal(t, want, body.Backup)
			}
		})
	}
}

// Живой API без блока backup создаёт кластер с выключенными бэкапами
// (backups list → 400 «backup disabled»), хотя спека обещает умолчание.
// Поэтому create всегда шлёт блок с умолчаниями спеки; отказ — только --no-backup.
func TestCreateBackupDefaults(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		flags []string
		want  string // JSON блока backup; "null" — блока нет
		err   string
	}{
		{name: "no flags sends spec defaults", want: `{"schedule":"0 3 * * SUN","retention_policy_days":14}`},
		{name: "days only keeps default schedule", flags: []string{"--backup-retention-days", "7"}, want: `{"schedule":"0 3 * * SUN","retention_policy_days":7}`},
		{name: "schedule only keeps default days", flags: []string{"--backup-schedule", "0 6 * * *"}, want: `{"schedule":"0 6 * * *","retention_policy_days":14}`},
		{name: "both flags", flags: []string{"--backup-schedule", "0 6 * * *", "--backup-retention-days", "30"}, want: `{"schedule":"0 6 * * *","retention_policy_days":30}`},
		{name: "explicit zero passes to API", flags: []string{"--backup-retention-days", "0"}, want: `{"schedule":"0 3 * * SUN","retention_policy_days":0}`},
		{name: "no-backup omits block", flags: []string{"--no-backup"}, want: `null`},
		{name: "body backup wins", flags: []string{"--body", `{"backup":{"retention_policy_days":5}}`}, want: `{"retention_policy_days":5}`},
		{name: "body null sends null", flags: []string{"--body", `{"backup":null}`}, want: `null`},
		{name: "no-backup with days", flags: []string{"--no-backup", "--backup-retention-days", "7"}, err: "[backup-retention-days no-backup] were all set"},
		{name: "no-backup with schedule", flags: []string{"--no-backup", "--backup-schedule", "0 6 * * *"}, err: "[backup-schedule no-backup] were all set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{
				"GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"BUSINESS"}]}`,
				"POST /v1/clusters":      `{"id":"op1"}`,
			})
			args := []string{"create", "--name", "n", "--version", "17", "--spec", "s1", "--initial-database", "app", "--storage-gib", "50", "--subnet-id", "sn1"}

			_, err := run(t, f, true, append(args, tc.flags...)...)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				assert.Empty(t, f.requests())

				return
			}

			require.NoError(t, err)

			var body map[string]any
			require.NoError(t, output.Decode([]byte(f.body("POST /v1/clusters")), &body))

			var want any
			require.NoError(t, output.Decode([]byte(tc.want), &want))
			assert.Equal(t, want, body["backup"])
		})
	}
}
