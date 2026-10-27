package cloud

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/gen/compute"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Compute — API виртуальных машин: подсети и зоны. Идентификаторы в нём — UUID,
// не UUID отказываем до запроса.
type Compute struct{ c *compute.Client }

// NewCompute — клиент API виртуальных машин по адресу server.
func NewCompute(server string, o Options) (*Compute, error) {
	c, err := compute.NewClient(server, compute.WithHTTPClient(o.HTTPClient), compute.WithRequestEditorFn(o.Auth),
		compute.WithRequestEditorFn(plusAsSpace))
	if err != nil {
		return nil, i18n.Errorf("cloud.err.client_compute", err)
	}

	return &Compute{c: c}, nil
}

// SubnetFilter — отбор подсетей; пустые поля не отправляются.
type SubnetFilter struct {
	Project, Zone, VPC, Name string
}

// SubnetCreate — новая подсеть; пустые необязательные поля не отправляются.
type SubnetCreate struct {
	Project, Name, Zone, CIDR, VPC, Description, Gateway string
	DNS                                                  []string
	Isolated                                             bool
}

// Subnets — подсети, все страницы: {"items": [...], "total": N}.
func (c *Compute) Subnets(ctx context.Context, f SubnetFilter) ([]byte, error) {
	project, err := parseUUID(i18n.T("cloud.resource.project"), f.Project)
	if err != nil {
		return nil, err
	}

	p := &compute.GetSubnetsSvcV1SubnetsGetParams{ProjectId: project, Name: optional(f.Name)}
	if f.Zone != "" {
		z, err := parseUUID(i18n.T("cloud.resource.zone"), f.Zone)
		if err != nil {
			return nil, err
		}

		p.AvailabilityZoneIds = &[]uuid.UUID{z}
	}

	if f.VPC != "" {
		v, err := parseUUID(i18n.T("cloud.resource.vpc"), f.VPC)
		if err != nil {
			return nil, err
		}

		p.VpcId = &v
	}

	return offsetPages("/api/v1/subnets", func(offset, limit int) ([]byte, error) {
		p.Offset, p.Limit = &offset, &limit

		return read(c.c.GetSubnetsSvcV1SubnetsGet(ctx, p))
	})
}

func (c *Compute) Subnet(ctx context.Context, id string) ([]byte, error) {
	u, err := parseUUID(i18n.T("cloud.resource.subnet"), id)
	if err != nil {
		return nil, err
	}

	return read(c.c.GetSubnetByIdSvcV1SubnetsSubnetIdGet(ctx, u))
}

// CreateSubnet создаёт подсеть. Зона — только id: availability_zone_name в
// спецификации есть, но API его отвергает (422 extra_forbidden, 14.10). Зона и
// VPC в теле — строки по спецификации, их формат проверяет API; UUID — только
// там, где его требует сгенерированный клиент (проект, путь, фильтр списка).
func (c *Compute) CreateSubnet(ctx context.Context, s SubnetCreate) ([]byte, error) {
	project, err := parseUUID(i18n.T("cloud.resource.project"), s.Project)
	if err != nil {
		return nil, err
	}

	req := compute.PublicSubnetCreateRequest{
		Name: s.Name, ProjectId: project, AvailabilityZoneId: &s.Zone, SubnetAddress: &s.CIDR,
		VpcId: optional(s.VPC), Description: optional(s.Description), DefaultGateway: optional(s.Gateway),
	}
	if len(s.DNS) > 0 {
		dns := make([]any, len(s.DNS))
		for i, d := range s.DNS {
			dns[i] = d
		}

		req.DnsServers = &dns
	}

	if s.Isolated {
		req.RoutedNetwork = ptr(false)
	}

	return read(c.c.CreateSubnetSvcV1SubnetsPost(ctx, req))
}

func (c *Compute) DeleteSubnet(ctx context.Context, id string) ([]byte, error) {
	u, err := parseUUID(i18n.T("cloud.resource.subnet"), id)
	if err != nil {
		return nil, err
	}

	return read(c.c.DeleteSubnetByIdSvcV1SubnetsSubnetIdDelete(ctx, u))
}

// Zones — зоны доступности: в API Redis и Kafka их списка нет, а спецификации
// без зоны не отдаются.
func (c *Compute) Zones(ctx context.Context) ([]byte, error) {
	return read(c.c.GetAvailabilityZoneListSvcV1AvailabilityZonesGet(ctx, nil))
}

func parseUUID(what, s string) (uuid.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, i18n.Errorf("cloud.err.id", what, s, ErrNotUUID)
	}

	return u, nil
}
