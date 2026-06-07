// NOTE: This package is experimental.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

type Server struct {
	AllowedDeploymentIds []string

	OnConnect    func(ctx context.Context, body ConnectBody) error
	OnDisconnect func(ctx context.Context, body DisconnectBody) error

	Client http.Client
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

// HandleEvents returns a http.Handler that will receive and verify cloud events sent from
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

		resp, err := s.Client.Do(req)
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

func (s *Server) SendMessage(ctx context.Context, message io.Reader, sendToken []byte) (*http.Response, error) {
	token, err := jwt.Parse(sendToken, jwt.WithVerify(false))
	if err != nil {
		return nil, fmt.Errorf("parse send token: %w", err)
	}

	deployment, ok := token.Issuer()
	if !ok {
		return nil, errors.New("no issuer on send token")
	}

	//TODO: Decide if checking deployment in AllowedDeploymentIds is meaningful here

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/send?send_token=%s", deployment, sendToken), message)
	if err != nil {
		return nil, fmt.Errorf("create send request: %w", err)
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do send request: %w", err)
	}

	return resp, nil
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
