package cluster

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		req     cloud.ClickHouseClusterCreate
		config  []string
		logging base.Logging
		body    string
		wait    bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: i18n.T("clickhouse.cluster.create.short"),
		Long:  i18n.T("clickhouse.cluster.create.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			if req.Project, err = s.NeedProject(); err != nil {
				return err
			}

			kv, err := base.ParseKV("config", config)
			if err != nil {
				return err
			}

			if len(kv) > 0 {
				if req.Config, err = cloud.ParseClickHouseConfig("config", kv); err != nil {
					return &base.ExitError{Code: 1, Msg: err.Error()}
				}
			}

			if req.Logging, err = logging.Value(); err != nil {
				return err
			}

			if req.Body, err = base.Body(body); err != nil {
				return err
			}

			if err := checkKeeper(&req); err != nil {
				return err
			}

			raw, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.CreateCluster(cmd.Context(), req) })
			if err != nil {
				return err
			}

			id, err := base.AwaitCreated(cmd, s, raw, wait, operation.Ops, operation.GetCmd, "clickhouse clusters list")
			if err != nil || id == "" {
				return err
			}

			cl, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Cluster(cmd.Context(), id) })
			if err != nil {
				return err
			}

			return s.Printer.Object(cl, func(w io.Writer) error { return printCluster(w, cl) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&req.Name, "name", "", i18n.T("clickhouse.cluster.create.flag.name"))
	f.StringVar(&req.Description, "description", "", i18n.T("common.flag.description"))
	f.StringVar(&req.Version, "version", "", i18n.T("clickhouse.cluster.create.flag.version"))
	f.StringVar(&req.Spec, "spec", "", i18n.T("clickhouse.cluster.create.flag.spec"))
	f.Int32Var(&req.Shards, "shards", 1, i18n.T("clickhouse.cluster.create.flag.shards"))
	f.Int32Var(&req.Replicas, "replicas", 1, i18n.T("clickhouse.cluster.create.flag.replicas"))
	f.Int32Var(&req.StorageGiB, "storage-gib", 0, i18n.T("clickhouse.cluster.create.flag.storage_gib"))
	f.StringSliceVar(&req.Subnets, "subnet-id", nil, i18n.T("clickhouse.cluster.create.flag.subnet_id"))
	f.StringVar(&req.KeeperMode, "keeper", "", i18n.T("clickhouse.cluster.create.flag.keeper"))
	keeperFlags(cmd, &req.Keeper)
	f.StringArrayVar(&config, "config", nil, i18n.T("clickhouse.cluster.create.flag.config"))
	logging.Register(cmd)
	f.StringVar(&body, "body", "", base.BodyHelpText())
	f.BoolVar(&wait, "wait", false, i18n.T("clickhouse.cluster.create.flag.wait"))

	for _, n := range []string{"name", "version", "spec"} {
		_ = cmd.MarkFlagRequired(n)
	}

	return cmd
}

// checkKeeper — сочетания режима Keeper, топологии и подсетей, на которые API
// отвечает 400: отказываем до запроса.
func checkKeeper(req *cloud.ClickHouseClusterCreate) error {
	topology, err := req.Topology()
	if err != nil {
		return err
	}

	if topology.KeeperMode == nil {
		return nil
	}

	mode := *topology.KeeperMode
	if mode != "" {
		if mode, err = base.OneOf("keeper", mode, "embedded", "standalone"); err != nil {
			return err
		}
	}

	shards, replicas, subnets := topology.Shards, topology.Replicas, topology.Subnets

	spec, storage := topology.KeeperSpec, topology.KeeperStorageGiB
	switch {
	case mode == "embedded" && ((shards != nil && *shards != 1) || (replicas != nil && *replicas != 1) || (subnets != nil && len(*subnets) != 1)):
		return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.cluster.err.keeper_embedded")}
	case mode == "" && subnets != nil && len(*subnets) > 1:
		return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.cluster.err.keeper_legacy")}
	case mode == "standalone" && spec != nil && *spec == "":
		return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.cluster.err.keeper_resources")}
	case mode == "standalone" && ((shards != nil && (*shards < 1 || *shards > 3)) || (replicas != nil && *replicas != 3) || (subnets != nil && len(*subnets) != 3)):
		return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.cluster.err.keeper_topology")}
	case mode == "standalone" && subnets != nil && ((*subnets)[0] == (*subnets)[1] || (*subnets)[0] == (*subnets)[2] || (*subnets)[1] == (*subnets)[2]):
		return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.cluster.err.keeper_topology")}
	case ((spec != nil && *spec != "") || (storage != nil && *storage > 0)) && mode != "standalone":
		return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.cluster.err.keeper_standalone")}
	}

	return nil
}
