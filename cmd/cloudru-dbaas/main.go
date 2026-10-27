package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

func main() {
	// Ctrl-C отменяет контекст: --wait сразу пишет, что операция продолжается
	// в облаке, и выходит.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	go func() { <-ctx.Done(); stop() }() // второй Ctrl-C — сразу выход, если шаг не слушает ctx (clig.dev)

	if err := cmd.NewDBaaSRoot().ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("cloudru-dbaas.error", err))
		os.Exit(output.ExitCode(err))
	}
}
