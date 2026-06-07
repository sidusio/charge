package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jws"
)

type Server struct {
	AllowedDeploymentIds []string

	OnConnect    func(ctx context.Context, body ConnectBody) error
	OnDisconnect func(ctx context.Context, body DisconnectBody) error
}

type cloudEvent[Data any] struct {
	Specversion string    `json:"specversion"`
	Id          string    `json:"id"`
	Source      string    `json:"source"`
	Type        string    `json:"type"`
	Time        time.Time `json:"time"`
	Data        Data      `json:"data"`
}

type ConnectBody struct {
	ClientToken  string `json:"clientToken"`
	SendToken    string `json:"sendToken"`
	ConnectionId string `json:"connectionId"`
	Origin       string `json:"origin"`
}

type DisconnectBody struct {
	ConnectionId string `json:"connectionId"`
}

// HandleEvents returns a http.Handler that will receive and verify cloud events send from
// charge instances.
func (s *Server) HandleEvents() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var event cloudEvent[json.RawMessage]

		err = json.Unmarshal(body, &event)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if !slices.Contains(s.AllowedDeploymentIds, event.Source) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, event.Source+"/.well-known/jwks.json", http.NoBody)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		chargeKeys := jwk.NewSet()
		err = json.NewDecoder(resp.Body).Decode(&chargeKeys)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, err = jws.Verify([]byte(r.Header.Get("Webhook-signature")), jws.WithKeySet(chargeKeys), jws.WithDetachedPayload(body))
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		switch event.Type {
		case "charge.connected.v1":
			if s.OnConnect == nil {
				return
			}

			var data ConnectBody
			err := json.Unmarshal(event.Data, &data)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			s.OnConnect(ctx, data)
		case "charge.disconnected.v1":
			if s.OnDisconnect == nil {
				return
			}

			var data DisconnectBody
			err := json.Unmarshal(event.Data, &data)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			s.OnDisconnect(ctx, data)
		}
	})
}

func (s *Server) RegisterChargeAllowed(mux *http.ServeMux, cacheDurationSeconds int) {
	mux.HandleFunc("GET /.well-known/charge-allowed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"allowedDeploymentUrls": s.AllowedDeploymentIds,
			"cacheDurationSeconds":  cacheDurationSeconds,
		})
	})
}
