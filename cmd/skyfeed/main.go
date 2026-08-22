package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/j4v3l/SkyFeed/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(app.NewCLI(os.Stdout, os.Stderr).Execute(ctx, os.Args[1:]))
}
