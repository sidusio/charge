package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	"sidus.io/charge/internal/util"
)

func Run(ctx context.Context, log *slog.Logger, cfg Config) error {
	eg, ctx := errgroup.WithContext(ctx)

	mux := http.NewServeMux()

	privateKeys, err := cfg.SigningKeys()
	if err != nil {
		return fmt.Errorf("get signing keys: %w", err)
	}

	signer, err := NewSigner(privateKeys, cfg.DeploymentURL)
	if err != nil {
		return fmt.Errorf("create signer: %w", err)
	}

	bouncer := &Bouncer{
		sfGroup:       &util.SingleFlightGroup[BounceStatus]{},
		m:             &util.SyncMap[string, BounceStatus]{},
		deploymentURL: cfg.DeploymentURL,
		allowInsecure: cfg.AllowInsecureOrigins,
	}

	bi := &BackendIndex{
		backens:       make(map[string]*Backend),
		signer:        signer,
		deploymentURL: cfg.DeploymentURL,
	}

	sse := &SSE{
		Log:                   log.With("service", "sse"),
		MaxConnectionDuration: cfg.MaxConnectionDuration,
		BackendIndex:          bi,
		Bouncer:               bouncer,
	}

	ws := &WS{
		Log:                   log.With("service", "ws"),
		MaxConnectionDuration: cfg.MaxConnectionDuration,
		BackendIndex:          bi,
		Bouncer:               bouncer,
	}

	mux.HandleFunc("GET /sse", sse.Handle)
	mux.HandleFunc("GET /ws", ws.Handle)
	mux.HandleFunc("GET /.well-known/jwks.json", signer.JWKsHandler)
	mux.HandleFunc("POST /send", HandleSend(signer, bi))

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

func HandleSend(signer Signer, bi *BackendIndex) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		sendToken, err := signer.ParseAndValidateSendToken([]byte(r.URL.Query().Get("send_token")))
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		be, err := bi.GetBackend(ctx, sendToken.CallbackURL)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.Body.Close()
		r.Body = http.NoBody

		err = be.SendMessage(ctx, sendToken.ConnectionId, body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}
