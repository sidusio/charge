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

	privateKeys, err := cfg.SigningKeys()
	if err != nil {
		return fmt.Errorf("get signing keys: %w", err)
	}

	signer, err := NewSigner(privateKeys)
	if err != nil {
		return fmt.Errorf("create signer: %w", err)
	}

	bi := BackendIndex{
		backens: make(map[string]*Backend),
		sign:    signer.SignDetatched,
	}

	mux.HandleFunc("GET /.well-known/jwks.json", signer.JWKsHandler)
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()

		token := r.URL.Query().Get("token")
		if token == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		callbackURL := r.URL.Query().Get("callback_url")
		if callbackURL == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		be, err := bi.GetBackend(ctx, callbackURL)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		id := "abc"

		signals := make(chan Signal)
		defer close(signals)

		be.Connect(ctx, id, signals)
		defer be.Disconnect(ctx, id)

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
