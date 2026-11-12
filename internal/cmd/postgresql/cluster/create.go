package cluster

import (
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/postgresql/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		req      cloud.PostgreSQLClusterCreate
		backup   backupFlags
		recovery cloud.PostgreSQLRecovery
		pitr     string
		logging  base.Logging
		body     string
		wait     bool
	)

	cmd := &cobra.Command{
		Use: "create", Short: i18n.T("postgresql.cluster.create.short"), Long: i18n.T("postgresql.cluster.create.long"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			if req.Project, err = s.NeedProject(); err != nil {
				return err
			}

			if req.Logging, err = logging.Value(); err != nil {
				return err
			}

			if req.Body, err = base.Body(body); err != nil {
				return err
			}

			req.Backup = backup.createValue(cmd)
			if cmd.Flags().Changed("recover-from-cluster") || cmd.Flags().Changed("backup-id") || cmd.Flags().Changed("pitr") {
				req.Recovery = &recovery
			}

			if cmd.Flags().Changed("pitr") {
				parsed, err := time.Parse(time.RFC3339, pitr)
				if err != nil {
					return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.recovery.err.time")}
				}

				recovery.PITR = &parsed
			}

			top, err := req.Topology()
			if err != nil {
				return err
			}

			if top.Recovery != nil {
				switch {
				case top.Recovery.ClusterID == "":
					return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.recovery.err.cluster")}
				case top.Recovery.BackupID != "" && top.Recovery.PITR != nil:
					return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.recovery.err.selectors")}
				}
			}

			h, err := s.PostgreSQL()
			if err != nil {
				return err
			}

			specs, err := loadSpecs(cmd.Context(), h, top.Version)

			var sp specification
			if err != nil {
				s.Printer.Messagef("%s", i18n.T("postgresql.cluster.warning.specs", err))
			} else {
				sp, err = findSpec(specs, top.Spec)
				if err != nil {
					return err
				}
			}

			if err := checkTopology(top, sp); err != nil {
				return err
			}

			raw, err := h.CreateCluster(cmd.Context(), req)
			if err != nil {
				return err
			}

			id, err := base.AwaitCreated(cmd, s, raw, wait, operation.Ops, operation.GetCmd, "postgresql clusters list")
			if err != nil || id == "" {
				return err
			}

			cl, err := h.Cluster(cmd.Context(), id)
			if err != nil {
				return err
			}

			return s.Printer.Object(cl, func(w io.Writer) error { return printCluster(cmd.Context(), s, w, cl) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&req.Name, "name", "", i18n.T("postgresql.cluster.create.flag.name"))
	f.StringVar(&req.Description, "description", "", i18n.T("common.flag.description"))
	f.StringVar(&req.Version, "version", "", i18n.T("postgresql.cluster.create.flag.version"))
	f.StringVar(&req.Spec, "spec", "", i18n.T("postgresql.cluster.create.flag.spec"))
	f.StringVar(&req.Database, "initial-database", "", i18n.T("postgresql.cluster.create.flag.database"))
	f.StringVar(&req.LCCollate, "lc-collate", "", i18n.T("postgresql.flag.lc_collate"))
	f.StringVar(&req.LCCtype, "lc-ctype", "", i18n.T("postgresql.flag.lc_ctype"))
	f.Int32Var(&req.Instances, "instances", 1, i18n.T("postgresql.cluster.flag.instances"))
	f.Int32Var(&req.DataGiB, "storage-gib", 0, i18n.T("postgresql.cluster.create.flag.storage"))
	f.Int32Var(&req.WALGiB, "wal-gib", 0, i18n.T("postgresql.cluster.flag.wal"))
	f.StringSliceVar(&req.Subnets, "subnet-id", nil, i18n.T("postgresql.cluster.create.flag.subnets"))
	backup.registerCreate(cmd)
	f.StringVar(&recovery.ClusterID, "recover-from-cluster", "", i18n.T("postgresql.cluster.recovery.flag.cluster"))
	f.StringVar(&recovery.BackupID, "backup-id", "", i18n.T("postgresql.cluster.recovery.flag.backup"))
	f.StringVar(&pitr, "pitr", "", i18n.T("postgresql.cluster.recovery.flag.pitr"))
	cmd.MarkFlagsMutuallyExclusive("backup-id", "pitr")
	logging.Register(cmd)
	f.StringVar(&body, "body", "", base.BodyHelpText())
	f.BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	for _, name := range []string{"name", "version", "spec", "initial-database", "storage-gib", "subnet-id"} {
		_ = cmd.MarkFlagRequired(name)
	}

	return cmd
}
