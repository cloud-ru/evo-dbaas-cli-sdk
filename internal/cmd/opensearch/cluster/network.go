package cluster

import (
	"errors"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// networkHint дополняет 400 при создании тем, что делать: у OpenSearch два требования к сети,
// которых нет в спеке (живой вызов 16.11) — публичный SNAT-шлюз в зоне подсети
// и единственный активный DNS-сервер VPC, на который смотрят все подсети кластера.
func networkHint(err error) error {
	var e *cloud.APIError
	if !errors.As(err, &e) || e.Status != 400 {
		return err
	}

	msg := strings.ToLower(e.Message)
	switch {
	case strings.Contains(msg, "nat gateway"):
		return &base.ExitError{Code: 1, Msg: i18n.T("opensearch.cluster.err.no_nat", err.Error())}
	case strings.Contains(msg, "dns server"):
		return &base.ExitError{Code: 1, Msg: i18n.T("opensearch.cluster.err.no_dns", err.Error())}
	}

	return err
}
