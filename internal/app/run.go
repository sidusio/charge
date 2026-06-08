package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
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

	bi := BackendIndex{
		backens:       make(map[string]*Backend),
		signer:        signer,
		deploymentURL: cfg.DeploymentURL,
	}

	mux.HandleFunc("GET /.well-known/jwks.json", signer.JWKsHandler)
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()

		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		callback := r.URL.Query().Get("callback_url")
		if callback == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		callbackURL, err := url.Parse(callback)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		bounceErr := bouncer.Allowed(callbackURL)
		if bounceErr != nil {
			if nbErr, ok := bounceErr.(NotAllowedError); ok {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(time.Until(nbErr.MayTryAgainAfter).Seconds())))
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(bounceErr.Error()))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		be, err := bi.GetBackend(ctx, callback)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		signals := make(chan Signal)
		defer close(signals)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		sseStarted := make(chan struct{})
		startSSE := sync.OnceFunc(func() {
			defer close(sseStarted)
			log.Debug("Starting SSE stream", "backend", be.callbackUrl.String())
			// Set SSE headers
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			// Write an initial comment to establish the SSE stream
			fmt.Fprint(w, ":\n\n")
			flusher.Flush()
		})

		maxDurationReached := time.After(cfg.MaxConnectionDuration)

		wg := &sync.WaitGroup{}
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					log.Debug("Context cancelled, closing connection", "backend", be.callbackUrl.String())
					return
				case <-maxDurationReached:
					log.Debug("Max connection duration reached, closing connection", "backend", be.callbackUrl.String())
					return
				case s, ok := <-signals:
					if !ok {
						return
					}

					log.Debug("Received signal", "signal", s, "backend", be.callbackUrl.String())

					startSSE()

					// Format SSE event: "data: <payload>\n\n"
					_, err := fmt.Fprintf(w, "data: %s\n\n", s.Message)
					if err != nil {
						select {
						case s.Result <- fmt.Errorf("write: %w", err):
						case <-ctx.Done():
							return
						}
					}

					select {
					case s.Result <- nil:
					case <-ctx.Done():
						return
					}

					log.Debug("Sent signal", "signal", s)

					flusher.Flush()
				}
			}
		})

		log.Debug("Connecting backend", "backend", be.callbackUrl.String())
		id, err := be.Connect(ctx, token, origin, signals, cfg.MaxConnectionDuration)
		if err != nil {
			select {
			case <-sseStarted:
				log.Error("Failed to connect backend but sse started", "backend", be.callbackUrl.String(), "error", err, "connection_id", id)
			default:
				// If SSE hasn't started, we can still set the status code
				log.Error("Failed to connect backend", "backend", be.callbackUrl.String(), "error", err)
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}
		log.Debug("Backend connected", "backend", be.callbackUrl.String(), "connection_id", id)
		defer func() {
			if err = be.Disconnect(ctx, id); err != nil {
				log.Warn("Failed to disconnect backend", "backend", be.callbackUrl.String(), "error", err)
			}
		}()

		startSSE()

		wg.Wait()
		log.Debug("Connection closed", "backend", be.callbackUrl.String(), "connection_id", id, "backend", be.callbackUrl.String())
	})

	mux.HandleFunc("POST /send", func(w http.ResponseWriter, r *http.Request) {
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
