package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type WS struct {
	Log *slog.Logger

	MaxConnectionDuration time.Duration

	BackendIndex *BackendIndex
	Bouncer      *Bouncer
}

func (ws *WS) Handle(w http.ResponseWriter, r *http.Request) {
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

	bounceErr := ws.Bouncer.Allowed(callbackURL)
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

	be, err := ws.BackendIndex.GetBackend(ctx, callback)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		ws.Log.Warn("WebSocket upgrade failed", "error", err)
		return
	}
	defer c.CloseNow()

	signals := make(chan Signal)
	defer close(signals)

	maxDurationReached := time.After(ws.MaxConnectionDuration)

	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()

	wg := &sync.WaitGroup{}
	wg.Go(func() {
		defer cancelConn()
		for {
			select {
			case <-connCtx.Done():
				ws.Log.Debug("Context cancelled, closing connection", "backend", be.callbackUrl.String())
				return
			case <-maxDurationReached:
				ws.Log.Debug("Max connection duration reached, closing connection", "backend", be.callbackUrl.String())
				return
			case s, ok := <-signals:
				if !ok {
					return
				}

				ws.Log.Debug("Received signal", "signal", s, "backend", be.callbackUrl.String())

				err := c.Write(connCtx, websocket.MessageText, s.Message)
				if err != nil {
					select {
					case s.Result <- fmt.Errorf("write: %w", err):
					case <-connCtx.Done():
					}
					return
				}

				select {
				case s.Result <- nil:
				case <-connCtx.Done():
					return
				}

				ws.Log.Debug("Sent signal", "signal", s)
			}
		}
	})

	ws.Log.Debug("Connecting backend", "backend", be.callbackUrl.String())
	id, err := be.Connect(ctx, token, origin, signals, ws.MaxConnectionDuration)
	if err != nil {
		ws.Log.Error("Failed to connect backend", "backend", be.callbackUrl.String(), "error", err)
		c.Close(websocket.StatusInternalError, "backend connection failed")
		cancelConn()
		wg.Wait()
		return
	}
	ws.Log.Debug("Backend connected", "backend", be.callbackUrl.String(), "connection_id", id)
	defer func() {
		if err = be.Disconnect(ctx, id); err != nil {
			ws.Log.Warn("Failed to disconnect backend", "backend", be.callbackUrl.String(), "error", err)
		}
	}()

	wg.Go(func() {
		defer cancelConn()
		for {
			_, msg, err := c.Read(connCtx)
			if err != nil {
				ws.Log.Debug("WebSocket read error", "error", err, "backend", be.callbackUrl.String())
				return
			}

			ws.Log.Debug("Received client message", "backend", be.callbackUrl.String(), "message_length", len(msg))
			err = be.SendClientMessage(ctx, id, msg)
			if err != nil {
				ws.Log.Warn("Failed to forward client message", "backend", be.callbackUrl.String(), "error", err)
			}
		}
	})

	wg.Wait()
	ws.Log.Debug("Connection closed", "backend", be.callbackUrl.String(), "connection_id", id)
	c.Close(websocket.StatusNormalClosure, "")
}
