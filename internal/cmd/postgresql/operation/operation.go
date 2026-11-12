package operation

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// GetCmd — команда, которой дождаться операции PostgreSQL после обрыва --wait.
const GetCmd = "postgresql operations get"

// Ops — опрос операций PostgreSQL для --wait.
func Ops(s *state.State) (operation.Getter, error) {
	r, err := s.PostgreSQL()
	if err != nil {
		return nil, err
	}

	return r.Operations(), nil
}

// With — вызов клиента PostgreSQL: ошибка создания клиента возвращается как есть.
func With(s *state.State, call func(*cloud.PostgreSQL) ([]byte, error)) ([]byte, error) {
	c, err := s.PostgreSQL()
	if err != nil {
		return nil, err
	}

	return call(c)
}

// NewCmd — postgresql operations.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "operations", Aliases: []string{"op", "operation"}, Short: i18n.T("postgresql.operation.short")}
	c.AddCommand(getCmd(s), listCmd(s))

	return c
}
