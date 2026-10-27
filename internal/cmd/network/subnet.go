package network

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

type subnet struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SubnetAddress string `json:"subnet_address"`
	State         string `json:"state"`
	VpcID         string `json:"vpc_id"`
	Zone          struct {
		Name string `json:"name"`
	} `json:"availability_zone"`
}

func subnetsCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "subnets", Short: i18n.T("network.subnet.short")}
	c.AddCommand(subnetListCmd(s), base.DescribeCmd{
		Use: "get SUBNET_ID", Short: i18n.T("network.subnet.get.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			c, err := s.Compute()
			if err != nil {
				return nil, err
			}

			return c.Subnet(ctx, args[0])
		},
		Table: printSubnet,
	}.Cmd(s), subnetCreateCmd(s), subnetDeleteCmd(s))

	return c
}

func subnetListCmd(s *state.State) *cobra.Command {
	var zone, vpcID, name string

	cmd := &cobra.Command{
		Use: "list", Short: i18n.T("network.subnet.list.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, err := s.NeedProject()
			if err != nil {
				return err
			}

			f := cloud.SubnetFilter{Project: project, VPC: vpcID, Name: name}
			if zone != "" {
				if f.Zone, err = base.ZoneID(cmd.Context(), s, zone); err != nil {
					return err
				}
			}

			c, err := s.Compute()
			if err != nil {
				return err
			}

			raw, err := c.Subnets(cmd.Context(), f)
			if err != nil {
				return err
			}

			return s.Printer.Object(raw, func(w io.Writer) error {
				var r struct {
					Items []subnet `json:"items"`
				}

				_ = output.Decode(raw, &r)

				t := output.NewTable(w, "ID", "NAME", "CIDR", "ZONE", "STATE")
				for _, s := range r.Items {
					t.Row(s.ID, s.Name, s.SubnetAddress, s.Zone.Name, s.State)
				}

				return t.Flush()
			})
		},
	}
	cmd.Flags().StringVar(&zone, "zone", "", i18n.T("network.subnet.list.flag.zone"))
	cmd.Flags().StringVar(&vpcID, "vpc-id", "", i18n.T("network.subnet.list.flag.vpc_id"))
	cmd.Flags().StringVar(&name, "name", "", i18n.T("network.subnet.list.flag.name"))

	return cmd
}

func subnetCreateCmd(s *state.State) *cobra.Command {
	var (
		req      cloud.SubnetCreate
		zone     string
		wait     bool
		required = []string{"name", "zone", "cidr"}
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: i18n.T("network.subnet.create.short"),
		Long:  i18n.T("network.subnet.create.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			if req.Project, err = s.NeedProject(); err != nil {
				return err
			}

			if req.Zone, err = base.ZoneID(cmd.Context(), s, zone); err != nil {
				return err
			}

			c, err := s.Compute()
			if err != nil {
				return err
			}

			raw, err := c.CreateSubnet(cmd.Context(), req)
			if err != nil {
				return err
			}

			if wait {
				var sn subnet

				_ = output.Decode(raw, &sn)
				if raw, err = waitSubnet(cmd.Context(), s, c, sn.ID); err != nil {
					return err
				}
			}

			return s.Printer.Object(raw, func(w io.Writer) error { return printSubnet(w, raw) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&req.Name, "name", "", i18n.T("network.subnet.create.flag.name"))
	f.StringVar(&zone, "zone", "", i18n.T("network.subnet.create.flag.zone"))
	f.StringVar(&req.CIDR, "cidr", "", i18n.T("network.subnet.create.flag.cidr"))
	f.StringVar(&req.VPC, "vpc-id", "", i18n.T("network.subnet.create.flag.vpc_id"))
	f.StringVar(&req.Description, "description", "", i18n.T("common.flag.description"))
	f.StringVar(&req.Gateway, "gateway", "", i18n.T("network.subnet.create.flag.gateway"))
	f.StringArrayVar(&req.DNS, "dns", nil, i18n.T("network.subnet.create.flag.dns"))
	f.BoolVar(&req.Isolated, "isolated", false, i18n.T("network.subnet.create.flag.isolated"))
	f.BoolVar(&wait, "wait", false, i18n.T("network.subnet.create.flag.wait"))

	for _, n := range required {
		_ = cmd.MarkFlagRequired(n)
	}

	return cmd
}

func subnetDeleteCmd(s *state.State) *cobra.Command {
	var yes, wait bool

	cmd := &cobra.Command{
		Use: "delete SUBNET_ID", Short: i18n.T("network.subnet.delete.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return &base.ExitError{Code: 1, Msg: i18n.T("common.err.need_yes")}
			}

			c, err := s.Compute()
			if err != nil {
				return err
			}

			if _, err := c.DeleteSubnet(cmd.Context(), args[0]); err != nil {
				return err
			}

			if wait {
				err := operation.Until(cmd.Context(), i18n.T("network.subnet.delete.wait", args[0]), base.WaitOptions(s), func(ctx context.Context) (bool, error) {
					_, err := c.Subnet(ctx, args[0])
					if cloud.IsNotFound(err) {
						return true, nil
					}

					return false, cloud.PollError(err)
				})
				if err != nil {
					return output.OperationError(err, "")
				}
			}

			return s.Printer.Object(map[string]any{"deleted": args[0]}, func(io.Writer) error {
				s.Printer.Messagef("%s", i18n.T("network.subnet.deleted", args[0]))
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, i18n.T("common.flag.yes"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("network.subnet.delete.flag.wait"))

	return cmd
}

// waitSubnet ждёт, пока подсеть выйдет из creating.
func waitSubnet(ctx context.Context, s *state.State, c *cloud.Compute, id string) ([]byte, error) {
	var (
		last []byte
		sn   subnet
	)

	err := operation.Until(ctx, i18n.T("network.subnet.create.wait", id), base.WaitOptions(s), func(ctx context.Context) (bool, error) {
		raw, err := c.Subnet(ctx, id)
		if err != nil {
			return false, cloud.PollError(err)
		}

		last, sn = raw, subnet{}
		_ = output.Decode(raw, &sn)

		return sn.State != "creating" && sn.State != "", nil
	})
	if err != nil {
		return last, output.OperationError(err, "")
	}

	if sn.State != "created" {
		return last, &base.ExitError{Code: 1, Msg: i18n.T("network.subnet.err.state", id, sn.State)}
	}

	return last, nil
}

func printSubnet(w io.Writer, raw []byte) error {
	var sn subnet

	_ = output.Decode(raw, &sn)
	t := output.NewTable(w, "FIELD", "VALUE")
	t.Row("id", sn.ID)
	t.Row("name", sn.Name)
	t.Row("cidr", sn.SubnetAddress)
	t.Row("zone", sn.Zone.Name)
	t.Row("vpc_id", sn.VpcID)
	t.Row("state", sn.State)

	return t.Flush()
}
