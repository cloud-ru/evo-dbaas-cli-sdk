// Package user — redis users: файл на глагол.
package user

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — redis users.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "users", Aliases: []string{"user"}, Short: i18n.T("redis.user.short")}
	c.AddCommand(listCmd(s), createCmd(s), deleteCmd(s), describeCmd(s), updateCmd(s))

	return c
}

// user — пользователь в ответе API: роли — в granted_roles, у старых — в roles.
type user struct {
	Name         string `json:"name"`
	GrantedRoles *struct {
		Value *[]string `json:"value"`
	} `json:"granted_roles"`
	Roles *[]string `json:"roles"`
}

func (u user) roles() []string {
	if u.GrantedRoles != nil && u.GrantedRoles.Value != nil {
		return *u.GrantedRoles.Value
	}

	if u.Roles != nil {
		return *u.Roles
	}

	return nil
}
