package cluster

import (
	"context"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

type specification struct {
	ID       string `json:"id"`
	Mode     string `json:"deployment_mode"`
	MaxHosts int32  `json:"max_hosts"`
}

func loadSpecs(ctx context.Context, h *cloud.PostgreSQL, version string) ([]specification, error) {
	raw, err := h.Specs(ctx, cloud.PostgreSQLSpecFilter{Version: version})
	if err != nil {
		return nil, err
	}

	var list struct {
		Specifications []specification `json:"specifications"`
	}
	if err := output.Decode(raw, &list); err != nil {
		return nil, err
	}

	return list.Specifications, nil
}

func findSpec(specs []specification, id string) (specification, error) {
	for _, sp := range specs {
		if sp.ID == id {
			return sp, nil
		}
	}

	return specification{}, &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.err.spec_not_found", id)}
}

func checkTopology(top cloud.PostgreSQLTopology, sp specification) error {
	mode := "business"

	maxHosts := int32(3)
	if sp.Mode == "STANDARD" {
		maxHosts = 2
		mode = "standard"
	}

	if sp.MaxHosts > 0 && sp.MaxHosts < maxHosts {
		maxHosts = sp.MaxHosts
	}

	switch {
	case top.Legacy != nil:
		return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.err.legacy_subnet")}
	case top.Instances < 1:
		return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.err.min_instances", mode)}
	case top.Instances > maxHosts:
		return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.err.max_instances", mode, maxHosts)}
	case len(top.Subnets) == 0:
		return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.err.no_subnets", mode)}
	case sp.Mode == "STANDARD" && len(top.Subnets) > 1:
		return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.err.business_only")}
	case len(top.Subnets) > 1 && len(top.Subnets) != int(top.Instances):
		return &base.ExitError{Code: 1, Msg: i18n.T("postgresql.cluster.err.subnet_count", len(top.Subnets), top.Instances)}
	}

	return nil
}
