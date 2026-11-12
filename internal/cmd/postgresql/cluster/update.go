package cluster

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func updateCmd(s *state.State) *cobra.Command {
	var (
		u         cloud.PostgreSQLClusterUpdate
		backup    backupFlags
		instances int32
		subnets   []string
		body      string
		wait      bool
	)

	cmd := &cobra.Command{Use: "update CLUSTER_ID", Short: i18n.T("postgresql.cluster.update.short"), Long: i18n.T("postgresql.cluster.update.long"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("instances") {
			u.Instances = &instances
		}

		if cmd.Flags().Changed("subnet-id") {
			u.Subnets = &subnets
		}

		u.Backup = backup.value(cmd)
		if u.Spec == "" && u.Instances == nil && u.Subnets == nil && u.DataGiB == 0 && u.WALGiB == 0 && u.Backup == nil && body == "" {
			return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.update.err.no_changes")}
		}

		var err error
		if u.Body, err = base.Body(body); err != nil {
			return err
		}

		h, err := s.PostgreSQL()
		if err != nil {
			return err
		}

		raw, err := h.Cluster(cmd.Context(), args[0])
		if err != nil {
			return err
		}

		var current struct {
			cloud.PostgreSQLTopology

			Backup *cloud.PostgreSQLBackup `json:"backup"`

			Storage struct {
				DataGiB int32 `json:"pg_data_gb"`
				WALGiB  int32 `json:"pg_wal_gb"`
			} `json:"storage"`
		}
		if err := output.Decode(raw, &current); err != nil {
			return err
		}

		if u.Backup != nil && current.Backup != nil {
			if u.Backup.Schedule == nil {
				u.Backup.Schedule = current.Backup.Schedule
			}

			if u.Backup.RetentionPolicyDays == nil {
				u.Backup.RetentionPolicyDays = current.Backup.RetentionPolicyDays
			}
		}

		specs, err := loadSpecs(cmd.Context(), h, current.Version)

		var sp specification
		if err == nil {
			sp, err = findSpec(specs, current.Spec)
		}

		if err != nil {
			s.Printer.Messagef("%s", i18n.T("postgresql.cluster.warning.specs", err))
		}

		if u.WALGiB > 0 && u.DataGiB == 0 {
			u.DataGiB = current.Storage.DataGiB
		}

		if u.DataGiB > 0 && !cmd.Flags().Changed("wal-gib") && current.Storage.WALGiB > 0 {
			u.WALGiB = current.Storage.WALGiB
		}

		top, err := u.Topology(current.PostgreSQLTopology)
		if err != nil {
			return err
		}

		if sp.Mode == "STANDARD" && top.Spec != current.Spec {
			return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.err.standard_resize")}
		}

		if sp.ID != "" && top.Spec != current.Spec {
			sp, err = findSpec(specs, top.Spec)
			if err != nil {
				return err
			}
		}

		if err := checkTopology(top, sp); err != nil {
			return err
		}

		raw, err = h.UpdateCluster(cmd.Context(), args[0], u)
		if err != nil {
			return err
		}

		return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
	}}
	f := cmd.Flags()
	f.StringVar(&u.Spec, "spec", "", i18n.T("postgresql.cluster.flag.spec"))
	f.Int32Var(&instances, "instances", 0, i18n.T("postgresql.cluster.flag.instances"))
	f.StringSliceVar(&subnets, "subnet-id", nil, i18n.T("postgresql.cluster.flag.subnets"))
	f.Int32Var(&u.DataGiB, "storage-gib", 0, i18n.T("postgresql.cluster.flag.storage"))
	f.Int32Var(&u.WALGiB, "wal-gib", 0, i18n.T("postgresql.cluster.flag.wal"))
	backup.register(cmd)
	f.StringVar(&body, "body", "", base.BodyHelpText())
	f.BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	return cmd
}
