// Package iam — cloudru-dbaas iam: сервисные аккаунты, их ключи доступа и роли
// в проекте. Сценарий для агента: человек один раз даёт ключ с правом
// управлять аккаунтами проекта, агент выпускает себе отдельный аккаунт с ролью
// на один продукт и дальше работает его ключом.
package iam

import (
	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — iam.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{
		Use:   "iam",
		Short: i18n.T("iam.short"),
		Long:  i18n.T("iam.long"),
	}
	c.AddCommand(serviceAccountsCmd(s), accessKeysCmd(s), rolesCmd(s))

	return c
}

// with — вызов IAM API с клиентом из state.
func with(s *state.State, call func(*cloud.IAM) ([]byte, error)) ([]byte, error) {
	c, err := s.IAM()
	if err != nil {
		return nil, err
	}

	return call(c)
}

// object — ответ IAM как map: API отвечает snake_case, хотя в спецификации
// camelCase, поля читает pick.
func object(raw []byte) map[string]any {
	var m map[string]any

	_ = output.Decode(raw, &m)

	return m
}

// items — список из поля с одним из имён keys.
func items(raw []byte, keys ...string) []any {
	m := object(raw)
	for _, k := range keys {
		if l, ok := m[k].([]any); ok {
			return l
		}
	}

	return nil
}

// pick возвращает первое непустое строковое поле из m.
func pick(m any, keys ...string) string {
	mm, _ := m.(map[string]any)
	for _, k := range keys {
		if s, ok := mm[k].(string); ok && s != "" {
			return s
		}
	}

	return ""
}
