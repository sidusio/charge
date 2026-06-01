package app

import (
	"bytes"
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
		connections: make(map[string]chan<- Signal),
	}

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
		// Read ?token
		// Figure out who is backend

		issuer := ""

		id := "abc"

		// Create channel
		signals := make(chan Signal)
		defer close(signals)

		// Register channel in index
		board.Register(id, signals)
		// defer unregistartion from index
		defer board.Unregister(id)

		// POST /connected
		http.Post(fmt.Sprintf("%s/events", issuer), "text/plain", bytes.NewBufferString("connected"))
		// defer POST /disconnected
		defer http.Post(fmt.Sprintf("%s/events", issuer), "text/plain", bytes.NewBufferString("disconnected"))

		// Loop over channel and write
		for {
			select {
			case s, ok := <-signals:
				if !ok {
					return
				}
				_, err := w.Write(s.Message)
				if err != nil {
					select {
					case s.Result <- fmt.Errorf("write: %w", err):
					case <-ctx.Done():
						return
					}
				}
				close(s.Result)
			case <-ctx.Done():
				return
			}
		}
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
