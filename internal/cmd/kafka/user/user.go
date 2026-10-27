// Package user — kafka users: файл на глагол.
package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — kafka users.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "users", Aliases: []string{"user"}, Short: i18n.T("kafka.user.short")}
	c.AddCommand(listCmd(s), createCmd(s), deleteCmd(s), describeCmd(s), updateCmd(s))

	return c
}

// user — пользователь в ответе API: роли — в granted_roles, у старых — в roles.
type user struct {
	Name         string    `json:"name"`
	GrantedRoles *repeated `json:"granted_roles"`
	Roles        *repeated `json:"roles"`
}

type repeated struct {
	Value *[]string `json:"value"`
}

func (u user) roles() []string {
	for _, r := range []*repeated{u.GrantedRoles, u.Roles} {
		if r != nil && r.Value != nil {
			return *r.Value
		}
	}

	return nil
}
