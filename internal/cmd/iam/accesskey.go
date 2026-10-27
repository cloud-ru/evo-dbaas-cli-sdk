package iam

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/state"
)

func accessKeysCmd(s *state.State) *cobra.Command {
	c := &cobra.Command{Use: "access-keys", Short: i18n.T("iam.accesskey.short")}
	c.AddCommand(base.ListCmd{
		Use: "list SERVICE_ACCOUNT_ID", Short: i18n.T("iam.accesskey.list.short"), Args: cobra.ExactArgs(1),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return with(s, func(c *cloud.IAM) ([]byte, error) { return c.AccessKeys(ctx, args[0]) })
		},
		Table: func(w io.Writer, raw []byte) error {
			t := output.NewTable(w, "ID", "KEY_ID", "EXPIRES", "DESCRIPTION")
			for _, m := range items(raw, "access_keys", "accessKeys") {
				t.Row(pick(m, "id"), pick(m, "key_id", "keyId"), pick(m, "expired_at", "expiredAt"), pick(m, "description"))
			}

			return t.Flush()
		},
	}.Cmd(s), base.DescribeCmd{
		Use: "get SERVICE_ACCOUNT_ID ACCESS_KEY_ID", Short: i18n.T("iam.accesskey.get.short"), Args: cobra.ExactArgs(2),
		List: func(ctx context.Context, s *state.State, args []string) ([]byte, error) {
			return with(s, func(c *cloud.IAM) ([]byte, error) { return c.AccessKey(ctx, args[0], args[1]) })
		},
	}.Cmd(s), accessKeyCreateCmd(s), confirmCmd(s, confirm{
		Use: "delete SERVICE_ACCOUNT_ID ACCESS_KEY_ID", Short: i18n.T("iam.accesskey.delete.short"), Args: cobra.ExactArgs(2),
		Refuse: i18n.T("common.err.need_yes"), Flag: i18n.T("common.flag.yes"), Done: func(id string) string { return i18n.T("iam.accesskey.revoked", id) },
		Call: func(ctx context.Context, c *cloud.IAM, args []string) ([]byte, error) {
			return c.DeleteAccessKey(ctx, args[0], args[1])
		},
	}))

	return c
}

func accessKeyCreateCmd(s *state.State) *cobra.Command {
	var envFile, desc, ttl string

	cmd := &cobra.Command{
		Use:   "create SERVICE_ACCOUNT_ID",
		Short: i18n.T("iam.accesskey.create.short"),
		Long:  i18n.T("iam.accesskey.create.long"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := s.NeedProject()
			if err != nil {
				return err
			}
			// Файл создаём до запроса: если путь занят, ключ не выпускается зря.
			fh, err := os.OpenFile(envFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return &base.ExitError{Code: 1, Msg: i18n.T("iam.accesskey.err.env_file", err)}
			}

			raw, err := with(s, func(c *cloud.IAM) ([]byte, error) { return c.CreateAccessKey(cmd.Context(), args[0], desc, ttl) })
			if err != nil {
				fh.Close()
				os.Remove(envFile)

				return err
			}

			m := object(raw)

			keyID, secret := pick(m, "key_id", "keyId"), pick(m, "secret")
			if keyID == "" || secret == "" {
				fh.Close()
				os.Remove(envFile)

				return &base.ExitError{Code: 1, Msg: i18n.T("iam.accesskey.err.missing_secret")}
			}

			// env-файл — машинный формат, не текст пользователю: переводить нечего.
			_, werr := io.WriteString(fh, fmt.Sprintf("CLOUDRU_KEY_ID=%s\nCLOUDRU_KEY_SECRET=%s\nCLOUDRU_PROJECT_ID=%s\n",
				shellValue(keyID), shellValue(secret), shellValue(project)))
			if cerr := fh.Close(); werr == nil {
				werr = cerr
			}

			if werr != nil {
				return &base.ExitError{Code: 1, Msg: i18n.T("iam.accesskey.err.write", werr)}
			}

			out := map[string]any{"id": pick(m, "id"), "key_id": keyID, "expired_at": pick(m, "expired_at", "expiredAt"), "env_file": envFile}

			return s.Printer.Object(out, func(w io.Writer) error {
				fmt.Fprintf(w, "%s\n", i18n.T("iam.accesskey.created", keyID, envFile))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&envFile, "env-file", "", i18n.T("iam.accesskey.create.flag.env_file"))
	cmd.Flags().StringVar(&desc, "description", "", i18n.T("iam.accesskey.create.flag.description"))
	cmd.Flags().StringVar(&ttl, "ttl", "", i18n.T("iam.accesskey.create.flag.ttl"))
	_ = cmd.MarkFlagRequired("env-file")

	return cmd
}

// shellValue готовит значение для env-файла, который подключают через
// «. FILE»: безопасные символы — как есть (файл читает и docker --env-file),
// остальное — в одинарных кавычках. Набор символов секрета API не обещает,
// а «$(…)» или перевод строки в нём выполнились бы при source.
func shellValue(v string) string {
	if v != "" && strings.Trim(v, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-+/=:@") == "" {
		return v
	}

	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
