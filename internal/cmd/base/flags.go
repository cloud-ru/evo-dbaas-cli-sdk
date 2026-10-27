package base

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// BodyHelpText — справка флага --body на выбранном языке.
func BodyHelpText() string { return i18n.T("base.flag.body") }

// OneOf проверяет значение enum-флага до запроса: иначе API отвечает 400 без
// подсказки, а *_UNSPECIFIED проходит и сервер выбирает сам. Регистр и
// «_»/«-» не важны; возвращает значение в виде из справки.
func OneOf(flag, v string, allowed ...string) (string, error) {
	n := strings.ReplaceAll(strings.ToLower(v), "_", "-")
	for _, a := range allowed {
		if n == a {
			return a, nil
		}
	}

	return "", &ExitError{Code: 1, Msg: i18n.T("base.err.one_of", flag, v, strings.Join(allowed, " | "))}
}

// ParseKV разбирает повторяемый флаг вида ключ=значение; пустая карта — поле не отправлять.
func ParseKV(flag string, kvs []string) (map[string]string, error) {
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, &ExitError{Code: 1, Msg: i18n.T("base.err.key_value", flag, kv)}
		}

		out[k] = v
	}

	return out, nil
}

// Body — значение --body: JSON строкой или @файл; "" — nil.
func Body(v string) ([]byte, error) {
	if v == "" {
		return nil, nil
	}

	name, ok := strings.CutPrefix(v, "@")
	if !ok {
		return []byte(v), nil
	}

	b, err := os.ReadFile(name)
	if err != nil {
		return nil, &ExitError{Code: 1, Msg: i18n.T("base.err.body", err)}
	}

	return b, nil
}

// Logging — флаги интеграции с Клиентским логированием, одинаковые у трёх продуктов.
type Logging struct {
	enabled bool
	groupID string
}

// Register добавляет --logging и --log-group-id.
func (l *Logging) Register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&l.enabled, "logging", false, i18n.T("base.flag.logging"))
	cmd.Flags().StringVar(&l.groupID, "log-group-id", "", i18n.T("base.flag.log_group_id"))
}

// Value — настройка для запроса; без --logging поле не отправляется.
func (l *Logging) Value() (cloud.Logging, error) {
	if l.groupID != "" && !l.enabled {
		return cloud.Logging{}, &ExitError{Code: 1, Msg: i18n.T("base.err.logging")}
	}

	return cloud.Logging{Enabled: l.enabled, GroupID: l.groupID}, nil
}

// Password — флаги пароля. Флага с самим паролем нет: он виден в истории
// shell и в списке процессов.
type Password struct {
	env   string
	stdin bool
}

// Register добавляет --password-stdin и --password-env.
func (p *Password) Register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&p.stdin, "password-stdin", false, i18n.T("base.flag.password_stdin"))
	cmd.Flags().StringVar(&p.env, "password-env", "", i18n.T("base.flag.password_env"))
}

// RegisterNew — те же флаги для смены пароля.
func (p *Password) RegisterNew(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&p.stdin, "password-stdin", false, i18n.T("base.flag.new_password_stdin"))
	cmd.Flags().StringVar(&p.env, "password-env", "", i18n.T("base.flag.new_password_env"))
}

// Set — пароль задан флагом: для update без флагов пароль не меняется.
func (p *Password) Set() bool { return p.env != "" || p.stdin }

// Read — пароль из переменной окружения или первой строки stdin.
func (p *Password) Read(cmd *cobra.Command, s *state.State) (string, error) {
	var password string

	switch {
	case p.env != "" && p.stdin:
		return "", &ExitError{Code: 1, Msg: i18n.T("base.err.password_sources")}
	case p.env != "":
		password = s.Env(p.env)
	case p.stdin:
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", i18n.Errorf("base.err.stdin", err)
		}

		password = strings.TrimRight(line, "\r\n")
	}

	if password == "" {
		return "", &ExitError{Code: 1, Msg: i18n.T("base.err.password_required")}
	}

	return password, nil
}

type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ZoneID принимает id или имя зоны (ru.AZ-1) и возвращает id.
func ZoneID(ctx context.Context, s *state.State, v string) (string, error) {
	if !strings.Contains(strings.ToLower(v), "az") {
		return v, nil
	}

	c, err := s.Compute()
	if err != nil {
		return "", err
	}

	raw, err := c.Zones(ctx)
	if err != nil {
		return "", i18n.Errorf("base.err.zones", err)
	}

	var zs []zone
	if err := output.Decode(raw, &zs); err != nil {
		return "", &ExitError{Code: 1, Msg: i18n.T("base.err.zones_parse", err)}
	}

	for _, z := range zs {
		if strings.EqualFold(z.Name, v) {
			return z.ID, nil
		}
	}

	return "", &ExitError{Code: 1, Msg: i18n.T("base.err.zone_not_found", v)}
}
