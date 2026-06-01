package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/kelseyhightower/envconfig"
	"sidus.io/notman/internal/app"
)

const (
	appName = "NOTMAN"
)

func main() {
	ctx, cleanup := signal.NotifyContext(context.Background(), os.Interrupt)
	go func(ctx context.Context, cleanup context.CancelFunc) {
		<-ctx.Done()
		cleanup()
	}(ctx, cleanup)

	cfgPrefix := appName
	if os.Getenv(fmt.Sprintf("%s_NO_ENV_PREFIX", appName)) == "true" {
		cfgPrefix = ""
	}

	var cfg app.Config
	err := envconfig.Process(cfgPrefix, &cfg)
	if err != nil {
		slog.Error("failed to process env vars", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	err = app.Run(ctx, logger, cfg)
	if err != nil {
		logger.ErrorContext(ctx, "failed to run", "error", err)
		os.Exit(1)
	}
	logger.Debug("shutdown successful")
}
