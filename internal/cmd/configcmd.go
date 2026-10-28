package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/config"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

// configCmd — config list и get (§8.12): что возьмёт команда и откуда; set и use-profile —
// правка файла. Работает без ключа и без init: файл читает сам, ошибка файла — тот же текст,
// что у любой команды.
func (a *app) configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: i18n.T("config.short")}

	c.AddCommand(&cobra.Command{
		Use: "list", Short: i18n.T("config.list.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := a.describe(cmd)
			if err != nil {
				return err
			}

			return a.printer(cmd).Object(r, func(w io.Writer) error { return listTable(w, r) })
		},
	}, &cobra.Command{
		Use: "get <key>", Short: i18n.T("config.get.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.describe(cmd)
			if err != nil {
				return err
			}

			s, err := r.Get(args[0])
			if err != nil {
				return err
			}

			return a.printer(cmd).Object(s, func(w io.Writer) error {
				if _, err := fmt.Fprintln(w, s.Value); err != nil {
					return i18n.Errorf("output.err.write_output", err)
				}

				return nil
			})
		},
	}, &cobra.Command{
		Use: "set <key> <value>", Short: i18n.T("config.set.short"), Long: i18n.T("config.set.long"), Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			value := args[1]
			if value == "-" {
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && line == "" {
					return i18n.Errorf("base.err.stdin", err)
				}

				value = strings.TrimRight(line, "\r\n")
			}

			ch, err := config.Set(config.Options{Path: a.configPath, Profile: a.profile}, args[0], value)
			if err != nil {
				return err
			}

			p := a.printer(cmd)
			p.Messagef("%s", i18n.T("config.set.done", args[0], ch.Profile, ch.Path))

			if ch.ShadowedBy != "" {
				p.Messagef("%s", i18n.T("config.set.shadowed", ch.ShadowedBy))
			}

			return nil
		},
	}, &cobra.Command{
		Use: "use-profile <name>", Short: i18n.T("config.use_profile.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.UseProfile(config.Options{Path: a.configPath}, args[0])
			if err != nil {
				return err
			}

			p := a.printer(cmd)
			p.Messagef("%s", i18n.T("config.use_profile.done", args[0], path))

			if env := config.Env("CLOUDRU_PROFILE"); env != "" && env != args[0] {
				p.Messagef("%s", i18n.T("config.set.shadowed", "CLOUDRU_PROFILE"))
			}

			return nil
		},
	})

	return c
}

func (a *app) describe(cmd *cobra.Command) (config.Report, error) {
	r, err := config.Describe(config.Options{Path: a.configPath, Profile: a.profile, Warn: cmd.ErrOrStderr()})
	if err != nil {
		return config.Report{}, err
	}

	if cmd.Flags().Changed("project") {
		r.Override("project_id", a.project)
	}

	return r, nil
}

func (a *app) printer(cmd *cobra.Command) *output.Printer {
	return &output.Printer{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), JSON: a.jsonOut}
}

func listTable(w io.Writer, r config.Report) error {
	file := r.File
	if file == "" {
		file = i18n.T("config.list.no_path")
	} else if !r.FileExists {
		file = i18n.T("config.list.no_file", r.File)
	}

	t := output.NewTable(w, "KEY", "VALUE", "SOURCE")
	t.Row("(profile)", r.Profile, r.ProfileSource)
	t.Row("(file)", file, "")

	for _, s := range r.Settings {
		t.Row(s.Key, s.Value, s.Source)
	}

	return t.Flush()
}
