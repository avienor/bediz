package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/avienor/bediz/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	os.Exit(cli.New(os.Stdout, os.Stderr).Run(ctx, os.Args[1:]))
}
