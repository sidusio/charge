package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	"sidus.io/notman/internal/util"
)

func Run(ctx context.Context, log *slog.Logger, cfg Config) error {

	eg, ctx := errgroup.WithContext(ctx)

	mux := http.NewServeMux()

	server := &http.Server{
		Handler: http.MaxBytesHandler(mux, 1<<20 /* 1mb */),
		Addr:    fmt.Sprintf(":%d", cfg.Port),
	}

	eg.Go(func() error {
		err := util.ListenAndServe(ctx, server, time.Second*10)
		return util.Wrap("http server failed", err)
	})

	return eg.Wait()
}
