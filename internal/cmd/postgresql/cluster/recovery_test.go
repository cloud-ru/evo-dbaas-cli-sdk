package cluster

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

// Сетка проверяет типовые тела; здесь — отказ до любого запроса и границы
// сочетаний, включая --body поверх флагов и разрешённый спекой один cluster_id.
func TestCreateRecoveryRules(t *testing.T) {
	t.Parallel()

	utc := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		flags    []string
		want     string
		recovery *cloud.PostgreSQLRecovery
	}{
		{name: "backup needs cluster", flags: []string{"--backup-id", "b1"}, want: "recovery requires"},
		{name: "pitr needs cluster", flags: []string{"--pitr", "2026-11-07T12:00:00Z"}, want: "recovery requires"},
		{name: "empty cluster", flags: []string{"--recover-from-cluster", ""}, want: "recovery requires"},
		{name: "exclusive flags", flags: []string{"--recover-from-cluster", "source", "--backup-id", "b1", "--pitr", "2026-11-07T12:00:00Z"}, want: "[backup-id pitr] were all set"},
		{name: "exclusive empty flag", flags: []string{"--recover-from-cluster", "source", "--backup-id", "", "--pitr", "2026-11-07T12:00:00Z"}, want: "[backup-id pitr] were all set"},
		{name: "date only", flags: []string{"--recover-from-cluster", "source", "--pitr", "2026-11-07"}, want: "RFC 3339"},
		{name: "missing zone", flags: []string{"--recover-from-cluster", "source", "--pitr", "2026-11-07T12:00:00"}, want: "RFC 3339"},
		{name: "invalid date", flags: []string{"--recover-from-cluster", "source", "--pitr", "2026-02-30T12:00:00Z"}, want: "RFC 3339"},
		{name: "empty time", flags: []string{"--recover-from-cluster", "source", "--pitr", ""}, want: "RFC 3339"},
		{name: "body needs cluster", flags: []string{"--body", `{"recovery_spec":{"backup_id":"b1"}}`}, want: "recovery requires"},
		{name: "exclusive body", flags: []string{"--body", `{"recovery_spec":{"cluster_id":"source","backup_id":"b1","pitr":"2026-11-07T12:00:00Z"}}`}, want: "choose either"},
		{name: "body invalid time", flags: []string{"--body", `{"recovery_spec":{"cluster_id":"source","pitr":"yesterday"}}`}, want: "cannot parse"},
		{name: "no recovery"},
		{name: "cluster alone", flags: []string{"--recover-from-cluster", "source"}, recovery: &cloud.PostgreSQLRecovery{ClusterID: "source"}},
		{name: "backup", flags: []string{"--recover-from-cluster", "source", "--backup-id", "b1"}, recovery: &cloud.PostgreSQLRecovery{ClusterID: "source", BackupID: "b1"}},
		{name: "pitr UTC", flags: []string{"--recover-from-cluster", "source", "--pitr", "2026-11-07T12:00:00Z"}, recovery: &cloud.PostgreSQLRecovery{ClusterID: "source", PITR: &utc}},
		{name: "body overrides flags", flags: []string{"--recover-from-cluster", "flag", "--backup-id", "flag", "--body", `{"recovery_spec":{"cluster_id":"body","backup_id":"body"}}`}, recovery: &cloud.PostgreSQLRecovery{ClusterID: "body", BackupID: "body"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFake(map[string]string{
				"GET /v1/specifications": `{"specifications":[{"id":"s1","deployment_mode":"BUSINESS"}]}`,
				"POST /v1/clusters":      `{"id":"op1"}`,
			})
			args := []string{"create", "--name", "n", "--version", "17", "--spec", "s1", "--initial-database", "app", "--storage-gib", "50", "--subnet-id", "sn1"}

			out, err := run(t, f, true, append(args, tc.flags...)...)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				assert.Equal(t, 1, output.ExitCode(err))
				assert.Empty(t, f.requests())
				assert.Empty(t, out)

				return
			}

			require.NoError(t, err)
			assert.Contains(t, out, "op1")
			assert.Equal(t, []string{"GET /v1/specifications", "POST /v1/clusters"}, f.requests())

			var body struct {
				Recovery *cloud.PostgreSQLRecovery `json:"recovery_spec"`
			}
			require.NoError(t, output.Decode([]byte(f.body("POST /v1/clusters")), &body))
			assert.Equal(t, tc.recovery, body.Recovery)
		})
	}
}
