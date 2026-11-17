package cluster

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

type scheduleFlags struct {
	enabled         bool
	cron, retention string
}

func (f *scheduleFlags) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.enabled, "snapshots-enabled", true, i18n.T("opensearch.cluster.flag.snapshots_enabled"))
	cmd.Flags().StringVar(&f.cron, "snapshots-cron", "", i18n.T("opensearch.cluster.flag.snapshots_cron"))
	cmd.Flags().StringVar(&f.retention, "snapshots-retention-days", "", i18n.T("opensearch.cluster.flag.snapshots_retention"))
}

func (f *scheduleFlags) value(cmd *cobra.Command) *cloud.OpenSearchSchedule {
	if !cmd.Flags().Changed("snapshots-enabled") && !cmd.Flags().Changed("snapshots-cron") && !cmd.Flags().Changed("snapshots-retention-days") {
		return nil
	}

	v := &cloud.OpenSearchSchedule{Enabled: f.enabled}
	if cmd.Flags().Changed("snapshots-cron") {
		v.Cron = &f.cron
	}

	if cmd.Flags().Changed("snapshots-retention-days") {
		v.RetentionDays = &f.retention
	}

	return v
}
