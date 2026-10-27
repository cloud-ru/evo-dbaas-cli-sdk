package cluster

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		req     cloud.KafkaClusterCreate
		params  []string
		logging base.Logging
		body    string
		wait    bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: i18n.T("kafka.cluster.create.short"),
		Long:  i18n.T("kafka.cluster.create.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			if req.Project, err = s.NeedProject(); err != nil {
				return err
			}

			if req.Params, err = base.ParseKV("param", params); err != nil {
				return err
			}

			if req.Logging, err = logging.Value(); err != nil {
				return err
			}

			if req.Body, err = base.Body(body); err != nil {
				return err
			}

			raw, err := operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.CreateCluster(cmd.Context(), req) })
			if err != nil {
				return err
			}

			id, err := base.AwaitCreated(cmd, s, raw, wait, operation.Ops, operation.GetCmd, "kafka clusters list")
			if err != nil || id == "" {
				return err
			}

			cl, err := operation.With(s, func(k *cloud.Kafka) ([]byte, error) { return k.Cluster(cmd.Context(), id) })
			if err != nil {
				return err
			}

			return s.Printer.Object(cl, func(w io.Writer) error { return printCluster(w, cl) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&req.Name, "name", "", i18n.T("kafka.cluster.create.flag.name"))
	f.StringVar(&req.Description, "description", "", i18n.T("common.flag.description"))
	f.StringVar(&req.Version, "version-id", "", i18n.T("kafka.cluster.create.flag.version_id"))
	f.StringVar(&req.Spec, "spec", "", i18n.T("kafka.cluster.create.flag.spec"))
	f.Int32Var(&req.Brokers, "brokers", 1, i18n.T("kafka.cluster.create.flag.brokers"))
	f.Int32Var(&req.StorageGiB, "storage-gib", 0, i18n.T("kafka.cluster.create.flag.storage_gib"))
	f.StringVar(&req.Subnet, "subnet-id", "", i18n.T("kafka.cluster.create.flag.subnet_id"))
	f.BoolVar(&req.UI, "ui", false, i18n.T("kafka.cluster.create.flag.ui"))
	f.BoolVar(&req.CruiseControl, "cruise-control", false, i18n.T("kafka.cluster.create.flag.cruise_control"))
	f.BoolVar(&req.AutoRebalance, "auto-rebalance", false, i18n.T("kafka.cluster.create.flag.auto_rebalance"))
	f.StringArrayVar(&params, "param", nil, i18n.T("kafka.cluster.create.flag.param"))
	logging.Register(cmd)
	f.StringVar(&body, "body", "", base.BodyHelpText())
	f.BoolVar(&wait, "wait", false, i18n.T("kafka.cluster.create.flag.wait"))

	for _, n := range []string{"name", "version-id", "spec", "storage-gib", "subnet-id"} {
		_ = cmd.MarkFlagRequired(n)
	}

	return cmd
}
