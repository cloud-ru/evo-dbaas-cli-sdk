package cluster

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Умолчания для create. Спека: «по умолчанию запускается каждое воскресенье
// в 03:00», срок хранения 14 дней; строки cron в спеке нет (example — 0 3 * * 0),
// "0 3 * * SUN" — то, что живой API сам ставил при неполном блоке (7.11).
// Без блока backup живой API бэкапы не включает, поэтому create шлёт их сам.
const (
	defaultBackupSchedule = "0 3 * * SUN"
	defaultBackupDays     = int32(14)
)

type backupFlags struct {
	schedule string
	days     int32
	disabled bool
}

func (b *backupFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&b.schedule, "backup-schedule", "", i18n.T("postgresql.cluster.flag.backup_schedule"))
	cmd.Flags().Int32Var(&b.days, "backup-retention-days", 0, i18n.T("postgresql.cluster.flag.backup_retention"))
}

func (b *backupFlags) value(cmd *cobra.Command) *cloud.PostgreSQLBackup {
	var backup cloud.PostgreSQLBackup
	if cmd.Flags().Changed("backup-schedule") {
		backup.Schedule = &b.schedule
	}

	if cmd.Flags().Changed("backup-retention-days") {
		backup.RetentionPolicyDays = &b.days
	}

	if backup.Schedule == nil && backup.RetentionPolicyDays == nil {
		return nil
	}

	return &backup
}

// registerCreate добавляет к флагам бэкапа --no-backup; только для create.
func (b *backupFlags) registerCreate(cmd *cobra.Command) {
	b.register(cmd)
	cmd.Flags().BoolVar(&b.disabled, "no-backup", false, i18n.T("postgresql.cluster.flag.no_backup"))
	cmd.MarkFlagsMutuallyExclusive("no-backup", "backup-schedule")
	cmd.MarkFlagsMutuallyExclusive("no-backup", "backup-retention-days")
}

// createValue — блок backup для create: незаданные поля берутся из умолчаний
// спеки; nil только при --no-backup.
func (b *backupFlags) createValue(cmd *cobra.Command) *cloud.PostgreSQLBackup {
	if b.disabled {
		return nil
	}

	backup := b.value(cmd)
	if backup == nil {
		backup = &cloud.PostgreSQLBackup{}
	}

	if backup.Schedule == nil {
		schedule := defaultBackupSchedule
		backup.Schedule = &schedule
	}

	if backup.RetentionPolicyDays == nil {
		days := defaultBackupDays
		backup.RetentionPolicyDays = &days
	}

	return backup
}
