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
	jsonOut    bool
	project    string
	configPath string
	profile    string
	timeout    time.Duration
	st         *state.State
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

			if withoutConfig(cmd) {
				return nil
			}

			return a.init(cmd)
		},
	}
	a.commonFlags(root)

	// Адреса продуктов — из конфига при выполнении (init): файл и профиль известны только после разбора флагов.
	root.AddCommand(clickhousecmd.NewCmd(a.st, ""), rediscmd.NewCmd(a.st, ""), kafkacmd.NewCmd(a.st, ""), iamcmd.NewCmd(a.st), networkcmd.NewCmd(a.st), zonescmd.NewCmd(a.st), a.configCmd())

	return root
}

func (a *app) commonFlags(root *cobra.Command) {
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, i18n.T("cmd.flag.json"))
	root.PersistentFlags().StringVar(&a.project, "project", "", i18n.T("cmd.flag.project"))
	root.PersistentFlags().StringVar(&a.configPath, "config", "", i18n.T("cmd.flag.config"))
	root.PersistentFlags().StringVar(&a.profile, "profile", "", i18n.T("cmd.flag.profile"))
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

// withoutConfig — help и дополнение работают без конфига (§8.7): кривой файл
// или опечатка в --profile не должны ломать справку и Tab. config читает файл сам и без ключа.
func withoutConfig(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "help", "completion", "config", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return true
		}
	}

	return false
}

// newApp выбирает язык до сборки дерева команд: тексты --help — уже на нём.
func newApp() *app {
	i18n.SetLanguage(config.Lang())

	return &app{st: &state.State{Printer: &output.Printer{Out: os.Stdout, Err: os.Stderr}}}
}

func init() {
	// go install …@vX.Y.Z собирает без ldflags — версию берём из модуля.
	if bi, ok := debug.ReadBuildInfo(); ok && Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		Version = bi.Main.Version
	}
}

// init читает конфиг после разбора флагов (--config, --profile) и заполняет
// то, что не задано флагом: флаг > env > файл > значение по умолчанию.
// Флаг, заданный явно пустым (--project "$P" при пустой P), остаётся пустым:
// иначе скрипт молча уйдёт в проект из env или профиля.
func (a *app) init(cmd *cobra.Command) error {
	cfg, err := config.Load(config.Options{Path: a.configPath, Profile: a.profile, Warn: cmd.ErrOrStderr()})
	if err != nil {
		return err
	}

	product := ""
	for c := cmd; c.HasParent(); c = c.Parent() {
		product = c.Name()
	}

	for _, f := range []struct {
		v   *string
		def string
		set bool
	}{
		{&a.project, cfg.ProjectID, cmd.Flags().Changed("project")},
		{&a.st.ClickHouseURL, cfg.ClickHouseURL, product == "clickhouse" && cmd.Flags().Changed("api-url")},
		{&a.st.RedisURL, cfg.RedisURL, product == "redis" && cmd.Flags().Changed("api-url")},
		{&a.st.KafkaURL, cfg.KafkaURL, product == "kafka" && cmd.Flags().Changed("api-url")},
	} {
		if !f.set {
			*f.v = f.def
		}
	}

	if cmd.Flags().Changed("api-url") && cmd.Flag("api-url").Value.String() == "" {
		return &base.ExitError{Code: 1, Msg: i18n.T("cmd.err.empty_api_url")}
	}

	a.st.Printer = &output.Printer{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), JSON: a.jsonOut}
	a.st.Project, a.st.Timeout, a.st.PollDelay, a.st.Env = a.project, a.timeout, pollDelay, config.Env

	return a.st.Connect(state.Conn{Config: cfg, Version: Version, RetryDelays: retryDelays, MaxBody: maxBody})
}

// pollDelay — шаг опроса операции; тесты уменьшают его.
var pollDelay = 5 * time.Second

// retryDelays и maxBody — настройки транспорта; nil и 0 — умолчания transport, тесты их меняют.
var (
	retryDelays []time.Duration
	maxBody     int64
)
