// Package cmd — корень: команды cloudru-dbaas поверх Public API Managed ClickHouse, Redis и Kafka.
package cmd

import (
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	clickhousecmd "github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/clickhouse"
	iamcmd "github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/iam"
	kafkacmd "github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka"
	networkcmd "github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/network"
	rediscmd "github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/redis"
	zonescmd "github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/zones"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/config"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

// Version подставляется при сборке: -ldflags "-X .../internal/cmd.Version=v0.1.0".
var Version = "dev"

type app struct {
	cfg     config.Config
	cfgErr  error
	jsonOut bool
	project string
	timeout time.Duration
	st      *state.State
}

// NewDBaaSRoot — один бинарник на три продукта: clickhouse, redis, kafka.
// Общие ключ, кэш токена, вывод и ожидание операций; у каждого продукта свой адрес API.
func NewDBaaSRoot() *cobra.Command {
	a := newApp()
	root := &cobra.Command{
		Use:           "cloudru-dbaas",
		Short:         i18n.T("cmd.short"),
		Long:          i18n.T("cmd.long"),
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectNegative(cmd); err != nil {
				return err
			}

			return a.init(cmd)
		},
	}
	a.commonFlags(root)

	root.AddCommand(clickhousecmd.NewCmd(a.st, a.cfg.ClickHouseURL), rediscmd.NewCmd(a.st, a.cfg.RedisURL), kafkacmd.NewCmd(a.st, a.cfg.KafkaURL), iamcmd.NewCmd(a.st), networkcmd.NewCmd(a.st), zonescmd.NewCmd(a.st))

	return root
}

func (a *app) commonFlags(root *cobra.Command) {
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, i18n.T("cmd.flag.json"))
	root.PersistentFlags().StringVar(&a.project, "project", a.cfg.ProjectID, i18n.T("cmd.flag.project"))
	root.PersistentFlags().DurationVar(&a.timeout, "timeout", 30*time.Minute, i18n.T("cmd.flag.timeout"))
}

// rejectNegative отказывает на отрицательном числовом флаге: все они —
// размеры и количества, а «0 — не менять» в resize пропустил бы -5 мимо
// проверки «что менять» и отправил пустой PATCH, создав операцию-пустышку.
func rejectNegative(cmd *cobra.Command) error {
	var err error
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if t := f.Value.Type(); err == nil && strings.HasPrefix(t, "int") && strings.HasPrefix(f.Value.String(), "-") {
			err = &base.ExitError{Code: 1, Msg: i18n.T("cmd.err.negative", f.Name, f.Value.String())}
		}
	})

	return err
}

// newApp читает конфиг один раз: из него — значения флагов по умолчанию и адреса API.
// Ошибку конфига вернёт init, у NewDBaaSRoot нет своего пути для ошибки.
func newApp() *app {
	cfg, err := config.Load()
	i18n.SetLanguage(cfg.Lang)

	return &app{cfg: cfg, cfgErr: err, st: &state.State{Printer: &output.Printer{Out: os.Stdout, Err: os.Stderr}}}
}

func init() {
	// go install …@vX.Y.Z собирает без ldflags — версию берём из модуля.
	if bi, ok := debug.ReadBuildInfo(); ok && Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		Version = bi.Main.Version
	}
}

func (a *app) init(cmd *cobra.Command) error {
	if a.cfgErr != nil {
		return a.cfgErr
	}

	a.st.Printer = &output.Printer{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), JSON: a.jsonOut}
	a.st.Project, a.st.Timeout, a.st.PollDelay, a.st.Env = a.project, a.timeout, pollDelay, config.Env

	return a.st.Connect(state.Conn{Config: a.cfg, Version: Version, RetryDelays: retryDelays, MaxBody: maxBody})
}

// pollDelay — шаг опроса операции; тесты уменьшают его.
var pollDelay = 5 * time.Second

// retryDelays и maxBody — настройки транспорта; nil и 0 — умолчания transport, тесты их меняют.
var (
	retryDelays []time.Duration
	maxBody     int64
)
