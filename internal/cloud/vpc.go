package cloud

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/gen/vpc"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

// VPC — Evolution VPC API: сети проекта и их операции.
type VPC struct{ c *vpc.Client }

// VPCCreate — новая VPC; пустое описание не отправляется.
type VPCCreate struct {
	Project, Name, Description string
}

// NewVPC — клиент VPC API по адресу server.
func NewVPC(server string, o Options) (*VPC, error) {
	c, err := vpc.NewClient(server, vpc.WithHTTPClient(o.HTTPClient), vpc.WithRequestEditorFn(o.Auth),
		vpc.WithRequestEditorFn(plusAsSpace))
	if err != nil {
		return nil, i18n.Errorf("cloud.err.client_vpc", err)
	}

	return &VPC{c: c}, nil
}

// List — VPC проекта, все страницы.
func (v *VPC) List(ctx context.Context, project string) ([]byte, error) {
	return tokenPages("/v1/vpcs", []string{"vpcs"}, func(token string) ([]byte, error) {
		return read(v.c.VpcServiceList(ctx, &vpc.VpcServiceListParams{ProjectId: project, PageSize: ptr[uint32](pageSize), PageToken: optional(token)}))
	})
}

func (v *VPC) Get(ctx context.Context, id string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.vpc"), id); err != nil {
		return nil, err
	}

	return read(v.c.VpcServiceGet(ctx, id))
}

// Create возвращает операцию создания.
func (v *VPC) Create(ctx context.Context, req VPCCreate) ([]byte, error) {
	return read(v.c.VpcServiceCreate(ctx, vpc.CreateVPCRequest{ProjectId: req.Project, Name: req.Name, Description: optional(req.Description)}))
}

// Delete возвращает операцию удаления.
func (v *VPC) Delete(ctx context.Context, id string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.vpc"), id); err != nil {
		return nil, err
	}

	return read(v.c.VpcServiceDelete(ctx, id))
}

func (v *VPC) Operation(ctx context.Context, id string) ([]byte, error) {
	if err := pathID(i18n.T("cloud.resource.operation"), id); err != nil {
		return nil, err
	}

	return read(v.c.OperationServiceGet(ctx, id))
}

// Operations — опрос операций VPC для operation.Waiter.
func (v *VPC) Operations() operation.Getter {
	return func(ctx context.Context, id string) (operation.Operation, error) {
		raw, err := v.Operation(ctx, id)
		if err != nil {
			return operation.Operation{}, PollError(err)
		}

		if op, ok := ParseVPCOperation(raw); ok {
			return op, nil
		}

		return operation.Operation{}, errors.New(i18n.T("cloud.err.empty_vpc_operation", id))
	}
}

type vpcOperation struct {
	ID         string  `json:"id"`
	Done       bool    `json:"done"`
	ResourceID string  `json:"resourceId"`
	Error      *rpcErr `json:"error"`
}

// ParseVPCOperation разбирает операцию VPC; ok — в теле операция с id.
func ParseVPCOperation(raw []byte) (op operation.Operation, ok bool) {
	op.Raw = raw

	var v vpcOperation
	if json.Unmarshal(raw, &v) != nil || v.ID == "" {
		return op, false
	}

	op.ID, op.Done, op.ResourceID = v.ID, v.Done, v.ResourceID
	if v.Error != nil {
		op.Failed, op.Message = true, v.Error.Message
	}

	return op, true
}
