package cluster

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func createCmd(s *state.State) *cobra.Command {
	var (
		req     cloud.RedisClusterCreate
		params  []string
		logging base.Logging
		body    string
		wait    bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: i18n.T("redis.cluster.create.short"),
		Long:  i18n.T("redis.cluster.create.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			if req.Project, err = s.NeedProject(); err != nil {
				return err
			}

			if req.Type, err = base.OneOf("type", req.Type, "standalone", "master-replica"); err != nil {
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

			raw, err := operation.With(s, func(r *cloud.Redis) ([]byte, error) { return r.CreateCluster(cmd.Context(), req) })
			if err != nil {
				return err
			}

			id, err := base.AwaitCreated(cmd, s, raw, wait, operation.Ops, operation.GetCmd, "redis clusters list")
			if err != nil || id == "" {
				return err
			}

			cl, err := operation.With(s, func(r *cloud.Redis) ([]byte, error) { return r.Cluster(cmd.Context(), id) })
			if err != nil {
				return err
			}

			return s.Printer.Object(cl, func(w io.Writer) error { return printCluster(w, cl) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&req.Name, "name", "", i18n.T("redis.cluster.create.flag.name"))
	f.StringVar(&req.Description, "description", "", i18n.T("common.flag.description"))
	f.StringVar(&req.Version, "version-id", "", i18n.T("redis.cluster.create.flag.version_id"))
	f.StringVar(&req.Spec, "spec", "", i18n.T("redis.cluster.create.flag.spec"))
	f.StringVar(&req.Type, "type", "standalone", i18n.T("redis.cluster.create.flag.type"))
	f.Int32Var(&req.Shards, "shards", 1, i18n.T("redis.cluster.create.flag.shards"))
	f.Int32Var(&req.Replicas, "replicas", 1, i18n.T("redis.cluster.create.flag.replicas"))
	f.Int32Var(&req.StorageGiB, "storage-gib", 0, i18n.T("redis.cluster.create.flag.storage_gib"))
	f.StringSliceVar(&req.Subnets, "subnet-id", nil, i18n.T("redis.cluster.create.flag.subnet_id"))
	f.StringArrayVar(&params, "param", nil, i18n.T("redis.cluster.create.flag.param"))
	logging.Register(cmd)
	f.StringVar(&body, "body", "", base.BodyHelpText())
	f.BoolVar(&wait, "wait", false, i18n.T("redis.cluster.create.flag.wait"))

	for _, n := range []string{"name", "version-id", "spec"} {
		_ = cmd.MarkFlagRequired(n)
	}

	return cmd
}
