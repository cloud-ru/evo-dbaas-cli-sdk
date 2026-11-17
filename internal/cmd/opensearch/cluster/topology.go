package cluster

import (
	"context"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

type specification struct {
	MinData      *int32 `json:"min_data_nodes"`
	MaxData      *int32 `json:"max_data_nodes"`
	MinMaster    *int32 `json:"min_master_nodes"`
	MaxMaster    *int32 `json:"max_master_nodes"`
	MinDataGiB   *int32 `json:"min_data_nodes_storage_gigs"`
	MinMasterGiB *int32 `json:"min_master_nodes_storage_gigs"`
}

func checkTopology(ctx context.Context, s *state.State, h *cloud.OpenSearch, top cloud.OpenSearchTopology) error {
	if top.Legacy != nil {
		return &base.ExitError{Code: 1, Msg: i18n.T("opensearch.cluster.err.legacy_subnet")}
	}

	raw, err := h.Spec(ctx, top.Spec)
	if cloud.IsNotFound(err) {
		return &base.ExitError{Code: 1, Msg: i18n.T("opensearch.cluster.err.spec_not_found", top.Spec)}
	}

	var sp specification
	if err == nil {
		err = output.Decode(raw, &sp)
	}

	if err != nil {
		s.Printer.Messagef("%s", i18n.T("opensearch.cluster.warning.spec", err))
		return nil
	}

	var msg string
	switch {
	case top.DataNodes != nil && sp.MinData != nil && *top.DataNodes < *sp.MinData:
		msg = i18n.T("opensearch.cluster.err.min_data_nodes", *top.DataNodes, *sp.MinData)
	case top.DataNodes != nil && sp.MaxData != nil && *top.DataNodes > *sp.MaxData:
		msg = i18n.T("opensearch.cluster.err.max_data_nodes", *top.DataNodes, *sp.MaxData)
	case top.MasterNodes != nil && sp.MinMaster != nil && *top.MasterNodes < *sp.MinMaster:
		msg = i18n.T("opensearch.cluster.err.min_master_nodes", *top.MasterNodes, *sp.MinMaster)
	case top.MasterNodes != nil && sp.MaxMaster != nil && *top.MasterNodes > *sp.MaxMaster:
		msg = i18n.T("opensearch.cluster.err.max_master_nodes", *top.MasterNodes, *sp.MaxMaster)
	case top.DataGiB != nil && sp.MinDataGiB != nil && *top.DataGiB < *sp.MinDataGiB:
		msg = i18n.T("opensearch.cluster.err.min_data_storage", *top.DataGiB, *sp.MinDataGiB)
	case top.MasterGiB != nil && sp.MinMasterGiB != nil && *top.MasterGiB < *sp.MinMasterGiB:
		msg = i18n.T("opensearch.cluster.err.min_master_storage", *top.MasterGiB, *sp.MinMasterGiB)
	}

	if msg != "" {
		return &base.ExitError{Code: 1, Msg: msg}
	}

	return nil
}
