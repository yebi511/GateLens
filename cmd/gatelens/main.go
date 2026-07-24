package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gatelens/gatelens/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, app.ConfigFromEnv()); err != nil {
		log.Fatal(err)
	}
}
