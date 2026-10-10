package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/denwilliams/recipes-cli/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], cli.Dependencies{})
	stop()
	os.Exit(code)
}
