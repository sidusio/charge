package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/jwx-go/jwkfetch/v4"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"golang.org/x/sync/errgroup"
	"sidus.io/notman/internal/util"
)

func Run(ctx context.Context, log *slog.Logger, cfg Config) error {
	eg, ctx := errgroup.WithContext(ctx)

	mux := http.NewServeMux()

	cache, err := jwkfetch.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return fmt.Errorf("create new cache: %w", err)
	}

	for _, allowedIssuer := range cfg.AllowlistedIssuers {
		err = cache.Register(ctx, allowedIssuer, jwkfetch.WithMinInterval(15*time.Second))
		if err != nil {
			return fmt.Errorf("register allowed issuer in cache: %w", err)
		}
	}

	bi := BackendIndex{
		backens:  make(map[string]*Backend),
		jwkCache: cache,
	}

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()

		rawToken := r.URL.Query().Get("token")
		if rawToken == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		insecureToken, err := jwt.ParseInsecure([]byte(rawToken))
		if err != nil {
			log.Warn("Failed to parse token", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		audience, _ := insecureToken.Audience()
		if !slices.Contains(audience, cfg.DeploymentIdentifier) {
			log.Warn("Invalid audience", "audience", audience)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		issuer, ok := insecureToken.Issuer()
		if !ok {
			log.Warn("Missing issuer")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		keyset, err := cache.Lookup(ctx, issuer)
		if err != nil {
			log.Warn("Keyset lookup failed", "issuer", issuer)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		token, err := jwt.Parse([]byte(rawToken), jwt.WithKeySet(keyset))
		if err != nil {
			log.Warn("Could not parse or validate token", "token", rawToken)
			w.WriteHeader(http.StatusBadRequest)
			return

		}

		be, err := bi.GetBackend(ctx, issuer)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		id, ok := token.Subject()
		if !ok {
			log.Warn("Missing subject")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

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
