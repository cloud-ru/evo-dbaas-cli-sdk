package cluster

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		req                    cloud.OpenSearchClusterCreate
		schedule               scheduleFlags
		masterNodes, masterGiB int32
		restore                bool
		source, snapshot, body string
		wait                   bool
	)

	cmd := &cobra.Command{Use: "create", Short: i18n.T("opensearch.cluster.create.short"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		var err error
		if req.Project, err = s.NeedProject(); err != nil {
			return err
		}

		if req.Body, err = base.Body(body); err != nil {
			return err
		}

		if cmd.Flags().Changed("master-nodes") {
			req.MasterNodes = &masterNodes
		}

		if cmd.Flags().Changed("master-storage-gib") {
			req.MasterGiB = &masterGiB
		}

		req.Schedule = schedule.value(cmd)
		if cmd.Flags().Changed("restore") || cmd.Flags().Changed("restore-cluster-id") || cmd.Flags().Changed("restore-snapshot") {
			req.Restore = &cloud.OpenSearchRestore{Enabled: restore}
			if cmd.Flags().Changed("restore-cluster-id") {
				req.Restore.ClusterID = &source
			}

			if cmd.Flags().Changed("restore-snapshot") {
				req.Restore.SnapshotName = &snapshot
			}
		}

		top, err := req.Topology()
		if err != nil {
			return err
		}

		h, err := s.OpenSearch()
		if err != nil {
			return err
		}

		if err := checkTopology(cmd.Context(), s, h, top); err != nil {
			return err
		}

		raw, err := h.CreateCluster(cmd.Context(), req)
		if err != nil {
			return networkHint(err)
		}

		id, err := base.AwaitCreated(cmd, s, raw, wait, operation.Ops, operation.GetCmd, "opensearch clusters list")
		if err != nil || id == "" {
			return err
		}

		cl, err := h.Cluster(cmd.Context(), id)
		if err != nil {
			return err
		}

		return s.Printer.Object(cl, func(w io.Writer) error { return printCluster(w, cl) })
	}}
	f := cmd.Flags()
	f.StringVar(&req.Name, "name", "", i18n.T("opensearch.cluster.flag.name"))
	f.StringVar(&req.Description, "description", "", i18n.T("common.flag.description"))
	f.StringVar(&req.Version, "version", "", i18n.T("opensearch.cluster.flag.version"))
	f.StringVar(&req.Spec, "spec", "", i18n.T("opensearch.cluster.flag.spec"))
	f.Int32Var(&req.DataNodes, "data-nodes", 0, i18n.T("opensearch.cluster.flag.data_nodes"))
	f.Int32Var(&masterNodes, "master-nodes", 0, i18n.T("opensearch.cluster.flag.master_nodes"))
	f.Int32Var(&req.DataGiB, "data-storage-gib", 0, i18n.T("opensearch.cluster.flag.data_storage"))
	f.Int32Var(&masterGiB, "master-storage-gib", 0, i18n.T("opensearch.cluster.flag.master_storage"))
	f.StringSliceVar(&req.DataSubnets, "data-subnet-id", nil, i18n.T("opensearch.cluster.flag.data_subnets"))
	f.StringSliceVar(&req.MasterSubnets, "master-subnet-id", nil, i18n.T("opensearch.cluster.flag.master_subnets"))
	schedule.register(cmd)
	f.BoolVar(&restore, "restore", true, i18n.T("opensearch.cluster.flag.restore"))
	f.StringVar(&source, "restore-cluster-id", "", i18n.T("opensearch.cluster.flag.restore_cluster"))
	f.StringVar(&snapshot, "restore-snapshot", "", i18n.T("opensearch.cluster.flag.restore_snapshot"))
	f.StringVar(&body, "body", "", base.BodyHelpText())
	f.BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	for _, name := range []string{"name", "version", "spec", "data-nodes", "data-storage-gib"} {
		_ = cmd.MarkFlagRequired(name)
	}

	return cmd
}
