// Package auth — clickhouse auth: проверка ключа доступа.
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse/operation"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// NewCmd — clickhouse auth.
func NewCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "auth", Short: i18n.T("clickhouse.auth.short")}
	c.AddCommand(&cobra.Command{
		Use: "check", Short: i18n.T("clickhouse.auth.check.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := probe(cmd.Context(), s); err != nil {
				return err
			}
			// project_checked: без проекта проверен только токен — агент
			// не должен читать ok как «доступ к проекту есть» (16.10).
			return s.Printer.Object(map[string]any{
				"ok": true, "api_url": s.ClickHouseURL, "project_id": s.Project, "project_checked": s.Project != "",
			}, func(w io.Writer) error {
				fmt.Fprintf(w, "%s\n", i18n.T("clickhouse.auth.check.success", s.ClickHouseURL))

				if s.Project == "" {
					s.Printer.Messagef("%s", i18n.T("clickhouse.auth.check.no_project"))
				}

				return nil
			})
		},
	})

	return c
}

// probe: versions отвечает любому ключу с токеном. Доступ к проекту
// проверяем отдельно: 14.10 ключ с одной dbaas.redis.admin проходил
// versions и получал 403 на всё по проекту.
func probe(ctx context.Context, s *state.State) error {
	if _, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.Versions(ctx) }); err != nil || s.Project == "" {
		return err
	}

	_, err := operation.With(s, func(h *cloud.ClickHouse) ([]byte, error) { return h.ProbeProject(ctx, s.Project) })
	// Тот же 403 бывает и сбоем облака (zones), поэтому подсказка —
	// проверить роли, а не «точно нет роли».
	var ae *cloud.APIError
	if errors.As(err, &ae) && ae.Status == 403 && strings.Contains(ae.Message, "failed to verify project access") {
		return &base.ExitError{Code: 1, Msg: i18n.T("clickhouse.auth.err.project_access",
			s.Project, err.Error())}
	}

	return err
}
