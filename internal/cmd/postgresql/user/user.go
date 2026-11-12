package user

import (
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — postgresql users.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "users", Aliases: []string{"user"}, Short: i18n.T("postgresql.user.short")}
	c.AddCommand(listCmd(s), describeCmd(s), createCmd(s), updateCmd(s), deleteCmd(s))

	return c
}

type role struct {
	Name string `json:"name"`
}
type user struct {
	Name           string `json:"name"`
	GrantedRoles   []role `json:"granted_roles"`
	SupportedRoles []role `json:"supported_roles"`
}

func roleNames(roles []role) string {
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, r.Name)
	}

	return strings.Join(names, ",")
}

func done(s *state.State, raw []byte, err error, format string, a ...any) error {
	if err != nil {
		return err
	}

	return s.Printer.Object(raw, func(io.Writer) error { s.Printer.Messagef(format, a...); return nil })
}
