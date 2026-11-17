package cluster

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/opensearch/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func updateCmd(s *state.State) *cobra.Command {
	var (
		u                                          cloud.OpenSearchClusterUpdate
		schedule                                   scheduleFlags
		dataNodes, masterNodes, dataGiB, masterGiB int32
		dataSubnets, masterSubnets                 []string
		body                                       string
		wait                                       bool
	)

	cmd := &cobra.Command{Use: "update CLUSTER_ID", Short: i18n.T("opensearch.cluster.update.short"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("data-nodes") {
			u.DataNodes = &dataNodes
		}

		if cmd.Flags().Changed("master-nodes") {
			u.MasterNodes = &masterNodes
		}

		if cmd.Flags().Changed("data-storage-gib") {
			u.DataGiB = &dataGiB
		}

		if cmd.Flags().Changed("master-storage-gib") {
			u.MasterGiB = &masterGiB
		}

		if cmd.Flags().Changed("data-subnet-id") {
			u.DataSubnets = &dataSubnets
		}

		if cmd.Flags().Changed("master-subnet-id") {
			u.MasterSubnets = &masterSubnets
		}

		u.Schedule = schedule.value(cmd)
		if u.Spec == "" && u.DataNodes == nil && u.MasterNodes == nil && u.DataGiB == nil && u.MasterGiB == nil && u.DataSubnets == nil && u.MasterSubnets == nil && u.Schedule == nil && body == "" {
			return &base.ExitError{Code: 1, Msg: i18n.T("opensearch.cluster.update.err.no_changes")}
		}

		var err error
		if u.Body, err = base.Body(body); err != nil {
			return err
		}

		patch, err := u.Topology(cloud.OpenSearchTopology{})
		if err != nil {
			return err
		}

		if patch.Legacy != nil {
			return &base.ExitError{Code: 1, Msg: i18n.T("opensearch.cluster.err.legacy_subnet")}
		}

		h, err := s.OpenSearch()
		if err != nil {
			return err
		}

		if patch.Spec != "" || patch.DataNodes != nil || patch.MasterNodes != nil || patch.DataGiB != nil || patch.MasterGiB != nil {
			raw, err := h.Cluster(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			var current cloud.OpenSearchTopology
			if err := output.Decode(raw, &current); err != nil {
				return err
			}

			top, err := u.Topology(current)
			if err != nil {
				return err
			}

			if err := checkTopology(cmd.Context(), s, h, top); err != nil {
				return err
			}
		}

		raw, err := h.UpdateCluster(cmd.Context(), args[0], u)
		if err != nil {
			return err
		}

		return base.Finish(cmd, s, raw, wait, operation.Ops, operation.GetCmd)
	}}
	f := cmd.Flags()
	f.StringVar(&u.Spec, "spec", "", i18n.T("opensearch.cluster.flag.spec"))
	f.Int32Var(&dataNodes, "data-nodes", 0, i18n.T("opensearch.cluster.flag.data_nodes"))
	f.Int32Var(&masterNodes, "master-nodes", 0, i18n.T("opensearch.cluster.flag.master_nodes"))
	f.Int32Var(&dataGiB, "data-storage-gib", 0, i18n.T("opensearch.cluster.flag.data_storage"))
	f.Int32Var(&masterGiB, "master-storage-gib", 0, i18n.T("opensearch.cluster.flag.master_storage"))
	f.StringSliceVar(&dataSubnets, "data-subnet-id", nil, i18n.T("opensearch.cluster.flag.data_subnets"))
	f.StringSliceVar(&masterSubnets, "master-subnet-id", nil, i18n.T("opensearch.cluster.flag.master_subnets"))
	schedule.register(cmd)
	f.StringVar(&body, "body", "", base.BodyHelpText())
	f.BoolVar(&wait, "wait", false, i18n.T("base.flag.wait"))

	return cmd
}
