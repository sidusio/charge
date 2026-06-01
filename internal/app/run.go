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

	board := &SwitchBoard{
		connections: make(map[string]chan<- []byte),
	}

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		// Read ?token
		// Figure out who is backend
		// Create channel
		// defer close(channel)
		// Register channel in index
		// defer unregistartion from index
		// POST /connected
		// defer POST /disconnected
		// Loop over channel and write
	})

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
