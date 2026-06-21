package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type SSE struct {
	Log *slog.Logger

	MaxConnectionDuration time.Duration
	MaxConnPerOrigin      int

	BackendIndex *BackendIndex
	Bouncer      *Bouncer
}

func (sse *SSE) Handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	bounceErr := sse.Bouncer.Allowed(callbackURL)
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

	be, err := sse.BackendIndex.GetBackend(ctx, callback)
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
		sse.Log.Debug("Starting SSE stream", "backend", be.callbackUrl.String())
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Write an initial comment to establish the SSE stream
		fmt.Fprint(w, ":\n\n")
		flusher.Flush()
	})

	maxDurationReached := time.After(sse.MaxConnectionDuration)

	wg := &sync.WaitGroup{}
	wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				sse.Log.Debug("Context cancelled, closing connection", "backend", be.callbackUrl.String())
				return
			case <-maxDurationReached:
				sse.Log.Debug("Max connection duration reached, closing connection", "backend", be.callbackUrl.String())
				return
			case s, ok := <-signals:
				if !ok {
					return
				}

				sse.Log.Debug("Received signal", "signal", s, "backend", be.callbackUrl.String())

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

				sse.Log.Debug("Sent signal", "signal", s)

				flusher.Flush()
			}
		}
	})

	sse.Log.Debug("Connecting backend", "backend", be.callbackUrl.String())
	id, err := be.Connect(ctx, token, origin, signals, sse.MaxConnectionDuration)
	if err != nil {
		select {
		case <-sseStarted:
			sse.Log.Error("Failed to connect backend but sse started", "backend", be.callbackUrl.String(), "error", err, "connection_id", id)
		default:
			// If SSE hasn't started, we can still set the status code
			sse.Log.Error("Failed to connect backend", "backend", be.callbackUrl.String(), "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}
	sse.Log.Debug("Backend connected", "backend", be.callbackUrl.String(), "connection_id", id)
	defer func() {
		if err = be.Disconnect(ctx, id); err != nil {
			sse.Log.Warn("Failed to disconnect backend", "backend", be.callbackUrl.String(), "error", err)
		}
	}()

	startSSE()

	wg.Wait()
	sse.Log.Debug("Connection closed", "backend", be.callbackUrl.String(), "connection_id", id, "backend", be.callbackUrl.String())
}
