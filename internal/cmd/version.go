package cmd

import (
	"io"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// versionCmd — синоним --version: тот же шаблон cobra, с --json — объект.
// Принтер — a.printer: при withoutConfig общий a.st.Printer не пересобирается.
func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: i18n.T("cmd.version.short"),
		Long:  i18n.T("cmd.version.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := cmd.Root()

			return a.printer(cmd).Object(map[string]string{"version": root.Version}, func(w io.Writer) error {
				// Шаблон cobra по умолчанию разбирается всегда: Must вместо ветки ошибки, недостижимой тестом.
				return template.Must(template.New("version").Parse(root.VersionTemplate())).Execute(w, root)
			})
		},
	}
}
